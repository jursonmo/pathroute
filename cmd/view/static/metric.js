(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.MetricDashboard = api;
  }
})(typeof window !== 'undefined' ? window : null, function () {
  const protocols = ['tcp', 'udp'];

  function number(value) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : NaN;
  }

  function simulationConfigStatus(count, dynamicMetricsEnabled) {
    if (!dynamicMetricsEnabled) {
      return '已加载 ' + count + ' 条协议配置；动态指标已关闭，当前路由仍使用静态 cost';
    }
    return '已加载 ' + count + ' 条协议配置';
  }

  function simulationSaveStatus(dynamicMetricsEnabled) {
    if (!dynamicMetricsEnabled) {
      return '配置已保存，将在服务端启用动态指标后生效';
    }
    return '配置已保存，服务端模拟器会按新周期继续运行';
  }

  function directedEdge(edge) {
    return {
      from: edge.from != null ? edge.from : edge.From,
      to: edge.to != null ? edge.to : edge.To,
      status: edge.status != null ? edge.status : edge.Status,
    };
  }

  function configKey(config) {
    return config.from_node_id + '->' + config.to_node_id + ':' + config.proto;
  }

  function defaultSimulationConfig(edge, proto) {
    return {
      from_node_id: edge.from,
      to_node_id: edge.to,
      proto: proto,
      enabled: false,
      interval_ms: 2000,
      latency_min_ms: 10,
      latency_max_ms: 100,
      packet_loss_min_ratio: 0,
      packet_loss_max_ratio: proto === 'udp' ? 0.05 : 0.01,
      rate_min_bps: 1000000,
      rate_max_bps: 10000000,
    };
  }

  function mergeSimulationConfigs(edges, savedConfigs) {
    const saved = new Map();
    (savedConfigs || []).forEach(function (config) {
      saved.set(configKey(config), config);
    });

    const result = [];
    (edges || []).map(directedEdge).filter(function (edge) {
      return edge.from && edge.to;
    }).sort(function (a, b) {
      return (a.from + '\u0000' + a.to).localeCompare(b.from + '\u0000' + b.to);
    }).forEach(function (edge) {
      protocols.forEach(function (proto) {
        const defaults = defaultSimulationConfig(edge, proto);
        const current = saved.get(configKey(defaults));
        // 每个协议创建独立对象，避免编辑 TCP 范围时意外修改同一条边的 UDP 配置。
        result.push(Object.assign({}, defaults, current || {}));
      });
    });
    return result;
  }

  function validateSimulationConfig(config) {
    if (!config.from_node_id || !config.to_node_id || config.from_node_id === config.to_node_id) {
      return '有向边起点和终点无效';
    }
    if (protocols.indexOf(String(config.proto).toLowerCase()) === -1) {
      return '协议必须是 TCP 或 UDP';
    }

    const interval = number(config.interval_ms);
    const latencyMin = number(config.latency_min_ms);
    const latencyMax = number(config.latency_max_ms);
    const lossMin = number(config.packet_loss_min_ratio);
    const lossMax = number(config.packet_loss_max_ratio);
    const rateMin = number(config.rate_min_bps);
    const rateMax = number(config.rate_max_bps);
    if (!Number.isInteger(interval) || interval <= 0) return '采样周期必须是大于 0 的整数毫秒';
    if (latencyMin < 0 || latencyMax < 0 || latencyMin > latencyMax) return '延迟最小值必须不大于最大值，且不能为负数';
    if (lossMin < 0 || lossMax > 1 || lossMin > lossMax) return '丢包率必须在 0～1 内，且最小值不能大于最大值';
    if (rateMin < 0 || rateMax < 0 || rateMin > rateMax) return '速率最小值必须不大于最大值，且不能为负数';
    if ([latencyMin, latencyMax, lossMin, lossMax, rateMin, rateMax].some(Number.isNaN)) {
      return '指标范围必须填写有效数字';
    }
    return '';
  }

  function buildSimulationPayload(configs) {
    return {
      configs: (configs || []).map(function (config) {
        const normalized = {
          from_node_id: String(config.from_node_id || '').trim(),
          to_node_id: String(config.to_node_id || '').trim(),
          proto: String(config.proto || '').toLowerCase(),
          enabled: Boolean(config.enabled),
          interval_ms: number(config.interval_ms),
          latency_min_ms: number(config.latency_min_ms),
          latency_max_ms: number(config.latency_max_ms),
          packet_loss_min_ratio: number(config.packet_loss_min_ratio),
          packet_loss_max_ratio: number(config.packet_loss_max_ratio),
          rate_min_bps: number(config.rate_min_bps),
          rate_max_bps: number(config.rate_max_bps),
        };
        const validationError = validateSimulationConfig(normalized);
        if (validationError) {
          throw new Error(normalized.from_node_id + ' → ' + normalized.to_node_id
            + ' (' + normalized.proto.toUpperCase() + ')：' + validationError);
        }
        return normalized;
      }),
    };
  }

  async function responseError(response, prefix) {
    let detail = '';
    try {
      detail = (await response.text()).trim();
    } catch (_) {
      detail = '';
    }
    throw new Error(prefix + '（HTTP ' + response.status + '）' + (detail ? '：' + detail : ''));
  }

  async function saveSimulationConfigs(fetchFn, configs, url) {
    const response = await fetchFn(url || '/api/metric-simulations', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(buildSimulationPayload(configs)),
    });
    if (!response.ok) await responseError(response, '保存模拟配置失败');
  }

  function protocolCost(snapshot, proto) {
    return ((snapshot && snapshot.protocol_costs) || []).find(function (item) {
      return item.proto === proto;
    });
  }

  function edgeMetricDisplay(edge, snapshot) {
    const status = Number(edge && (edge.status != null ? edge.status : edge.Status));
    const isAvailable = status === 1;
    const edgeCost = edge && (edge.cost != null ? edge.cost : edge.Cost);
    const edgeDegraded = edge && (edge.cost_degraded != null ? edge.cost_degraded : edge.CostDegraded);
    const edgeRevision = edge && (edge.cost_revision != null ? edge.cost_revision : edge.CostRevision);
    const degraded = snapshot ? Boolean(snapshot.is_degraded) : Boolean(edgeDegraded);
    const cost = snapshot && snapshot.cost != null ? snapshot.cost : (edgeCost != null ? edgeCost : 1000);
    const revision = snapshot && snapshot.revision != null ? snapshot.revision : (edgeRevision || 0);
    let availability = isAvailable ? '可用' : '不可用';
    if (isAvailable && degraded) availability = '可用（指标降级）';
    return {
      isAvailable: isAvailable,
      availability: availability,
      summary: availability + ' · cost ' + cost + ' · revision ' + revision,
      cost: cost,
      revision: revision,
      degraded: degraded,
      reason: snapshot && snapshot.degrade_reason
        ? snapshot.degrade_reason
        : (edge && (edge.degrade_reason || edge.DegradeReason) ? (edge.degrade_reason || edge.DegradeReason) : ''),
      tcp: protocolCost(snapshot, 'tcp'),
      udp: protocolCost(snapshot, 'udp'),
      latestMetricAt: snapshot && snapshot.latest_metric_at ? snapshot.latest_metric_at : '',
    };
  }

  function edgeGraphLabel(edge) {
    const effective = edge && (edge.cost != null ? edge.cost : edge.Cost);
    const staticCost = edge && (edge.static_cost != null ? edge.static_cost : edge.StaticCost);
    const revision = edge && (edge.cost_revision != null ? edge.cost_revision : edge.CostRevision);
    const degraded = Boolean(edge && (edge.cost_degraded != null ? edge.cost_degraded : edge.CostDegraded));
    if (staticCost == null && !revision && !degraded) return String(effective);
    let detail = staticCost != null ? '静态 ' + staticCost : '';
    if (revision) detail += (detail ? ' · ' : '') + 'r' + revision;
    if (degraded) detail += (detail ? ' · ' : '') + '指标降级';
    return String(effective) + (detail ? '\n' + detail : '');
  }

  function edgeEditableCost(edge) {
    if (!edge) return undefined;
    if (edge.static_cost != null) return edge.static_cost;
    if (edge.StaticCost != null) return edge.StaticCost;
    return edge.cost != null ? edge.cost : edge.Cost;
  }

  function applyEditedStaticCost(edge, cost) {
    if (!edge) return;
    const source = edge.cost_source != null ? edge.cost_source : edge.CostSource;
    const hasStaticField = edge.static_cost != null || edge.StaticCost != null;
    if (edge.StaticCost != null && edge.static_cost == null) edge.StaticCost = cost;
    else edge.static_cost = cost;
    // 动态模式只修改人工回退值；只有静态图的有效 cost 才随编辑立即改变。
    if (!hasStaticField || !source || source === 'static') {
      if (edge.Cost != null && edge.cost == null) edge.Cost = cost;
      else edge.cost = cost;
    }
  }

  function applyCostSnapshotsToEdges(graphEdges, payload) {
    const snapshots = new Map();
    ((payload && payload.edges) || []).forEach(function (snapshot) {
      snapshots.set(snapshot.from_node_id + '->' + snapshot.to_node_id, snapshot);
    });
    const changed = [];
    (graphEdges || []).forEach(function (edge) {
      const source = edge.cost_source != null ? edge.cost_source : edge.CostSource;
      if (!source || source === 'static') return;
      const directed = directedEdge(edge);
      const key = directed.from + '->' + directed.to;
      const snapshot = snapshots.get(key);
      if (!snapshot) return;
      const revision = snapshot.revision != null ? snapshot.revision : ((payload && payload.revision) || 0);
      const degraded = Boolean(snapshot.is_degraded);
      const reason = snapshot.degrade_reason || '';
      const didChange = edge.cost !== snapshot.cost
        || edge.cost_revision !== revision
        || Boolean(edge.cost_degraded) !== degraded
        || (edge.degrade_reason || '') !== reason;
      edge.cost = snapshot.cost;
      edge.dynamic_cost = snapshot.cost;
      edge.cost_revision = revision;
      edge.cost_degraded = degraded;
      edge.degrade_reason = reason;
      edge.cost_source = 'dynamic';
      if (didChange) changed.push(key);
    });
    return changed;
  }

  function formatLocalTime(value) {
    if (!value) return '—';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
  }

  function formatRouteMetadata(snapshot) {
    if (!snapshot || snapshot.available === false) {
      const error = snapshot && (snapshot.last_error || snapshot.LastError);
      return '暂无成功的路由计算结果' + (error ? ' · 最近错误 ' + error : '');
    }
    const revision = snapshot.cost_revision != null ? snapshot.cost_revision : 0;
    const version = snapshot.route_version != null ? snapshot.route_version : 0;
    const stale = (snapshot.is_stale || snapshot.stale) ? ' · 结果陈旧' : '';
    return 'cost revision ' + revision + ' · 路由版本 ' + version
      + ' · 计算时间 ' + formatLocalTime(snapshot.calculated_at) + stale;
  }

  function escapeHTML(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  async function fetchJSON(fetchFn, url) {
    const response = await fetchFn(url);
    if (!response.ok) await responseError(response, '加载失败');
    return response.json();
  }

  function renderConfigRows(container, configs) {
    if (!container) return;
    container.innerHTML = configs.map(function (config, index) {
      const last = config.last_metric;
      const latest = last
        ? formatLocalTime(last.observed_at) + ' · ' + Number(last.latency_ms).toFixed(1) + 'ms'
        : (config.last_generated_at ? formatLocalTime(config.last_generated_at) : '尚未生成');
      function input(field, value, step, min, max) {
        return '<input data-field="' + field + '" type="number" value="' + escapeHTML(value)
          + '" step="' + step + '"' + (min != null ? ' min="' + min + '"' : '')
          + (max != null ? ' max="' + max + '"' : '') + '>';
      }
      return '<div class="metric-config" data-metric-config="' + index + '">'
        + '<div class="metric-config-title"><label><input data-field="enabled" type="checkbox"'
        + (config.enabled ? ' checked' : '') + '> ' + escapeHTML(config.from_node_id) + ' → '
        + escapeHTML(config.to_node_id) + ' <b>' + escapeHTML(config.proto.toUpperCase()) + '</b></label></div>'
        + '<div class="metric-grid"><span>周期 ms</span>' + input('interval_ms', config.interval_ms, '1', '1')
        + '<span>延迟 ms</span><div class="metric-range">' + input('latency_min_ms', config.latency_min_ms, '0.1', '0')
        + '<i>～</i>' + input('latency_max_ms', config.latency_max_ms, '0.1', '0') + '</div>'
        + '<span>丢包率</span><div class="metric-range">' + input('packet_loss_min_ratio', config.packet_loss_min_ratio, '0.001', '0', '1')
        + '<i>～</i>' + input('packet_loss_max_ratio', config.packet_loss_max_ratio, '0.001', '0', '1') + '</div>'
        + '<span>速率 bps</span><div class="metric-range">' + input('rate_min_bps', config.rate_min_bps, '1', '0')
        + '<i>～</i>' + input('rate_max_bps', config.rate_max_bps, '1', '0') + '</div></div>'
        + '<div class="metric-latest">最近生成：' + escapeHTML(latest) + '</div></div>';
    }).join('');
  }

  function readConfigRows(container, baseConfigs) {
    return Array.from(container.querySelectorAll('[data-metric-config]')).map(function (row) {
      const index = Number(row.getAttribute('data-metric-config'));
      const config = Object.assign({}, baseConfigs[index]);
      row.querySelectorAll('[data-field]').forEach(function (input) {
        const field = input.getAttribute('data-field');
        config[field] = input.type === 'checkbox' ? input.checked : input.value;
      });
      return config;
    });
  }

  function renderEdgeOptions(select, edges) {
    if (!select) return;
    select.innerHTML = (edges || []).map(directedEdge).filter(function (edge) {
      return edge.from && edge.to;
    }).map(function (edge) {
      const value = edge.from + '->' + edge.to;
      return '<option value="' + escapeHTML(value) + '">' + escapeHTML(edge.from + ' → ' + edge.to) + '</option>';
    }).join('');
  }

  function renderMetricHistory(container, samples) {
    if (!container) return;
    if (!samples || !samples.length) {
      container.textContent = '该时间段没有指标';
      return;
    }
    container.innerHTML = '<div class="metric-table"><div class="metric-table-head">时间 / 协议 / 延迟 / 丢包 / 速率</div>'
      + samples.map(function (sample) {
        return '<div>' + escapeHTML(formatLocalTime(sample.observed_at)) + ' · '
          + escapeHTML(String(sample.proto || '').toUpperCase()) + ' · '
          + escapeHTML(Number(sample.latency_ms).toFixed(2)) + 'ms · '
          + escapeHTML((Number(sample.packet_loss_ratio) * 100).toFixed(2)) + '% · '
          + escapeHTML(Number(sample.rate_bps).toFixed(0)) + 'bps</div>';
      }).join('') + '</div>';
  }

  function renderEdgeCosts(container, graphEdges, payload) {
    if (!container) return;
    const snapshots = new Map();
    ((payload && payload.edges) || []).forEach(function (snapshot) {
      snapshots.set(snapshot.from_node_id + '->' + snapshot.to_node_id, snapshot);
    });
    container.innerHTML = (graphEdges || []).map(function (rawEdge) {
      const edge = directedEdge(rawEdge);
      const key = edge.from + '->' + edge.to;
      const source = rawEdge.cost_source != null ? rawEdge.cost_source : rawEdge.CostSource;
      // 静态模式可能保留旧动态快照，但当前路由权重必须与 /graph 的静态有效 cost 保持一致。
      const display = edgeMetricDisplay(rawEdge, source === 'static' ? null : snapshots.get(key));
      function costText(item) {
        if (!item) return '—';
        return String(item.cost) + (item.is_expired ? '（过期）' : '');
      }
      return '<div class="metric-cost ' + (display.isAvailable ? '' : 'metric-unavailable') + '">'
        + '<b>' + escapeHTML(edge.from + ' → ' + edge.to) + '</b><span>' + escapeHTML(display.summary) + '</span>'
        + '<small>TCP ' + escapeHTML(costText(display.tcp)) + ' / UDP ' + escapeHTML(costText(display.udp))
        + ' · 最近指标 ' + escapeHTML(formatLocalTime(display.latestMetricAt))
        + (display.reason ? ' · 原因 ' + escapeHTML(display.reason) : '') + '</small></div>';
    }).join('') || '暂无边';
  }

  function toDateTimeLocal(date) {
    const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
    return shifted.toISOString().slice(0, 16);
  }

  function initMetricDashboard(options) {
    if (typeof document === 'undefined') return null;
    const fetchFn = (options && options.fetch) || window.fetch.bind(window);
    const configList = document.getElementById('metric-simulation-list');
    if (!configList) return null;
    const status = document.getElementById('metric-config-status');
    const history = document.getElementById('metric-history-results');
    const costs = document.getElementById('metric-current-costs');
    const routeMeta = document.getElementById('metric-route-meta');
    const edgeSelect = document.getElementById('metric-history-edge');
    let graphEdges = [];
    let configs = [];
    let dynamicMetricsEnabled = false;

    function setStatus(message, isError) {
      if (!status) return;
      status.textContent = message;
      status.classList.toggle('metric-error', Boolean(isError));
    }

    async function loadConfigs() {
      setStatus('正在加载模拟配置…', false);
      try {
        const responses = await Promise.all([
          fetchJSON(fetchFn, '/graph'),
          fetchJSON(fetchFn, '/api/metric-simulations'),
        ]);
        graphEdges = responses[0].edges || responses[0].Edges || [];
        dynamicMetricsEnabled = responses[0].dynamic_metrics_enabled === true;
        configs = mergeSimulationConfigs(graphEdges, responses[1].configs || []);
        renderConfigRows(configList, configs);
        renderEdgeOptions(edgeSelect, graphEdges);
        setStatus(simulationConfigStatus(configs.length, dynamicMetricsEnabled), false);
      } catch (error) {
        setStatus('动态指标功能不可用：' + error.message, true);
      }
    }

    async function saveConfigs() {
      try {
        const updated = readConfigRows(configList, configs);
        await saveSimulationConfigs(fetchFn, updated);
        await loadConfigs();
        setStatus(simulationSaveStatus(dynamicMetricsEnabled), false);
      } catch (error) {
        setStatus(error.message, true);
      }
    }

    async function loadHistory() {
      try {
        const parts = String(edgeSelect.value || '').split('->');
        const proto = document.getElementById('metric-history-proto').value;
        const start = new Date(document.getElementById('metric-history-start').value);
        const end = new Date(document.getElementById('metric-history-end').value);
        if (parts.length !== 2 || Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || end <= start) {
          throw new Error('请选择有向边和有效的起止时间');
        }
        const params = new URLSearchParams({
          from: parts[0], to: parts[1], proto: proto,
          start: start.toISOString(), end: end.toISOString(), limit: '200',
        });
        history.textContent = '正在查询…';
        const payload = await fetchJSON(fetchFn, '/api/metrics?' + params.toString());
        renderMetricHistory(history, payload.samples || []);
      } catch (error) {
        history.textContent = '查询失败：' + error.message;
      }
    }

    async function refreshCurrent() {
      try {
        const responses = await Promise.all([
          fetchJSON(fetchFn, '/api/edge-costs'),
          fetchJSON(fetchFn, '/api/routes/latest'),
        ]);
        renderEdgeCosts(costs, graphEdges, responses[0]);
        routeMeta.textContent = formatRouteMetadata(responses[1]);
        if (typeof window.CustomEvent === 'function') {
          window.dispatchEvent(new window.CustomEvent('metric-costs-updated', { detail: responses[0] }));
        }
      } catch (error) {
        if (costs) costs.textContent = '当前状态加载失败：' + error.message;
      }
    }

    const now = new Date();
    const startInput = document.getElementById('metric-history-start');
    const endInput = document.getElementById('metric-history-end');
    if (startInput) startInput.value = toDateTimeLocal(new Date(now.getTime() - 10 * 60 * 1000));
    if (endInput) endInput.value = toDateTimeLocal(now);
    document.getElementById('btn-metric-config-reload').addEventListener('click', loadConfigs);
    document.getElementById('btn-metric-config-save').addEventListener('click', saveConfigs);
    document.getElementById('btn-metric-history').addEventListener('click', loadHistory);
    document.getElementById('btn-metric-refresh').addEventListener('click', refreshCurrent);

    loadConfigs().then(refreshCurrent);
    // 页面只负责轮询展示；指标生成始终由服务端后台任务执行，关闭浏览器不会停止模拟器。
    const timer = window.setInterval(refreshCurrent, 2000);
    return { reload: loadConfigs, refresh: refreshCurrent, stop: function () { window.clearInterval(timer); } };
  }

  if (typeof window !== 'undefined' && typeof document !== 'undefined') {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', function () { initMetricDashboard(); });
    } else {
      initMetricDashboard();
    }
  }

  return {
    validateSimulationConfig,
    mergeSimulationConfigs,
    buildSimulationPayload,
    saveSimulationConfigs,
    edgeMetricDisplay,
    edgeGraphLabel,
    edgeEditableCost,
    applyEditedStaticCost,
    applyCostSnapshotsToEdges,
    formatRouteMetadata,
    simulationConfigStatus,
    simulationSaveStatus,
    initMetricDashboard,
  };
});
