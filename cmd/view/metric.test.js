const assert = require('node:assert/strict');
const test = require('node:test');

const metric = require('./static/metric.js');

function validConfig(overrides) {
  return Object.assign({
    from_node_id: 'A',
    to_node_id: 'B',
    proto: 'tcp',
    enabled: true,
    interval_ms: 2000,
    latency_min_ms: 10,
    latency_max_ms: 20,
    packet_loss_min_ratio: 0,
    packet_loss_max_ratio: 0.05,
    rate_min_bps: 1000,
    rate_max_bps: 2000,
  }, overrides || {});
}

test('simulation config rejects reversed and out-of-range values', () => {
  assert.match(metric.validateSimulationConfig(validConfig({
    latency_min_ms: 21,
    latency_max_ms: 20,
  })), /延迟最小值/);
  assert.match(metric.validateSimulationConfig(validConfig({
    packet_loss_max_ratio: 1.01,
  })), /丢包率/);
  assert.match(metric.validateSimulationConfig(validConfig({
    interval_ms: 0,
  })), /采样周期/);
});

test('simulation configs keep TCP and UDP independent for one directed edge', () => {
  const saved = [validConfig({
    proto: 'udp',
    latency_min_ms: 30,
    latency_max_ms: 40,
  })];

  const got = metric.mergeSimulationConfigs([
    { from: 'A', to: 'B' },
  ], saved);

  assert.equal(got.length, 2);
  assert.deepEqual(got.map((item) => item.proto), ['tcp', 'udp']);
  assert.equal(got[0].latency_min_ms, 10);
  assert.equal(got[1].latency_min_ms, 30);
  assert.notEqual(got[0], got[1]);
});

test('batch payload validates every protocol config before saving', () => {
  const configs = [
    validConfig({ proto: 'tcp' }),
    validConfig({ proto: 'udp', rate_min_bps: 3000, rate_max_bps: 2000 }),
  ];

  assert.throws(
    () => metric.buildSimulationPayload(configs),
    /A → B \(UDP\).*速率最小值/,
  );
});

test('save failure exposes a useful Chinese error message', async () => {
  const fakeFetch = async () => ({
    ok: false,
    status: 500,
    text: async () => 'simulation storage unavailable',
  });

  await assert.rejects(
    metric.saveSimulationConfigs(fakeFetch, [validConfig()]),
    /保存模拟配置失败.*500.*simulation storage unavailable/,
  );
});

test('edge display distinguishes unavailable from available degraded cost 1000', () => {
  const snapshot = {
    cost: 1000,
    is_degraded: true,
    degrade_reason: 'metrics_expired',
    revision: 9,
    protocol_costs: [],
  };

  const unavailable = metric.edgeMetricDisplay({ status: 0 }, snapshot);
  const degraded = metric.edgeMetricDisplay({ status: 1 }, snapshot);

  assert.equal(unavailable.availability, '不可用');
  assert.equal(unavailable.isAvailable, false);
  assert.equal(degraded.availability, '可用（指标降级）');
  assert.equal(degraded.isAvailable, true);
  assert.match(degraded.summary, /cost 1000/);
  assert.match(degraded.summary, /revision 9/);
});

test('edge display keeps static cost when dynamic mode is disabled', () => {
  const display = metric.edgeMetricDisplay({
    status: 1,
    cost: 15,
    static_cost: 15,
    cost_source: 'static',
    cost_revision: 0,
  }, null);

  assert.equal(display.cost, 15);
  assert.equal(display.degraded, false);
  assert.equal(display.availability, '可用');
});

test('graph edge label shows effective cost, static cost, revision and degradation', () => {
  assert.equal(metric.edgeGraphLabel({
    cost: 1000,
    static_cost: 10,
    cost_revision: 9,
    cost_degraded: true,
  }), '1000\n静态 10 · r9 · 指标降级');
  assert.equal(metric.edgeGraphLabel({ cost: 10 }), '10');
});

test('edge editor uses static cost without replacing effective dynamic cost', () => {
  const dynamicEdge = {
    cost: 42,
    static_cost: 10,
    cost_source: 'dynamic',
  };

  assert.equal(metric.edgeEditableCost(dynamicEdge), 10);
  metric.applyEditedStaticCost(dynamicEdge, 15);
  assert.equal(dynamicEdge.static_cost, 15);
  assert.equal(dynamicEdge.cost, 42);

  const staticEdge = { cost: 10, static_cost: 10, cost_source: 'static' };
  metric.applyEditedStaticCost(staticEdge, 15);
  assert.equal(staticEdge.cost, 15);
  assert.equal(staticEdge.static_cost, 15);
});

test('cost polling updates dynamic graph edges but never overwrites static mode', () => {
  const edges = [
    { from: 'A', to: 'B', cost: 1000, static_cost: 10, cost_source: 'dynamic_fallback' },
    { from: 'B', to: 'C', cost: 20, static_cost: 20, cost_source: 'static' },
  ];
  const changed = metric.applyCostSnapshotsToEdges(edges, {
    revision: 8,
    edges: [
      { from_node_id: 'A', to_node_id: 'B', cost: 42, revision: 8, is_degraded: false },
      { from_node_id: 'B', to_node_id: 'C', cost: 99, revision: 8, is_degraded: false },
    ],
  });

  assert.deepEqual(changed, ['A->B']);
  assert.equal(edges[0].cost, 42);
  assert.equal(edges[0].static_cost, 10);
  assert.equal(edges[0].cost_revision, 8);
  assert.equal(edges[1].cost, 20);
});

test('route metadata includes cost revision and calculation time', () => {
  const got = metric.formatRouteMetadata({
    available: true,
    cost_revision: 12,
    route_version: 7,
    calculated_at: '2026-07-15T08:00:00Z',
    stale: false,
  });

  assert.match(got, /cost revision 12/);
  assert.match(got, /路由版本 7/);
  assert.match(got, /2026/);
});

test('simulation status does not claim the runner is active when dynamic metrics are disabled', () => {
  assert.match(metric.simulationConfigStatus(4, false), /动态指标已关闭/);
  assert.match(metric.simulationSaveStatus(false), /启用动态指标后生效/);
  assert.match(metric.simulationConfigStatus(4, true), /已加载 4 条/);
  assert.match(metric.simulationSaveStatus(true), /模拟器会按新周期/);
});
