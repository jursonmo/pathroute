(function () {
  const container = document.getElementById('graph');
  if (!container) return;

  const nodes = new vis.DataSet([]);
  const edges = new vis.DataSet([]);
  let fullNodes = [];
  let fullEdges = [];
  let network = null;
  let edgeClickTimeout = null;
  let editContext = null; // { type: 'node', id } | { type: 'edge', from, to }
  let addEdgeMode = false;
  let addEdgeFrom = null;

  const options = {
    nodes: {
      shape: 'dot',
      size: 20,
      font: { size: 16, color: '#fff' },
      borderWidth: 2,
      color: { background: '#0f3460', border: '#e94560' },
    },
    edges: {
      arrows: 'to',
      font: { size: 12, color: '#fff', align: 'middle' },
      color: { color: '#4a9eff' },
    },
    physics: { enabled: false },
    interaction: { dragNodes: true, dragView: true },
  };

  function edgeId(from, to) {
    return from + '->' + to;
  }

  function parseEdgeId(id) {
    const i = id.indexOf('->');
    if (i === -1) return null;
    return { from: id.slice(0, i), to: id.slice(i + 2) };
  }

  function nodeIdOf(n) {
    if (typeof n === 'string') return n;
    if (n == null) return '';
    return n.id != null ? n.id : n.ID;
  }

  function nodePosOf(n) {
    if (typeof n !== 'object' || n == null) return { x: undefined, y: undefined };
    const x = n.x != null ? n.x : n.X;
    const y = n.y != null ? n.y : n.Y;
    return { x, y };
  }

  function edgeFromToCost(e) {
    const from = e.from != null ? e.from : e.From;
    const to = e.to != null ? e.to : e.To;
    const cost = e.cost != null ? e.cost : e.Cost;
    return { from, to, cost };
  }

  function isBidi(from, to) {
    return fullEdges.some(function (e) {
      const ftc = edgeFromToCost(e);
      return ftc.from === to && ftc.to === from;
    });
  }

  function edgeSmoothFor(from, to) {
    if (!isBidi(from, to)) return undefined;
    const id = edgeId(from, to);
    const revId = edgeId(to, from);
    return id < revId ? false : { type: 'curvedCW', roundness: 0.4 };
  }

  function updateBidiStylesForPair(a, b) {
    const id1 = edgeId(a, b);
    const id2 = edgeId(b, a);
    const e1 = edges.get(id1);
    const e2 = edges.get(id2);
    if (e1) edges.update({ id: id1, smooth: edgeSmoothFor(a, b) });
    if (e2) edges.update({ id: id2, smooth: edgeSmoothFor(b, a) });
  }

  function setEdgeModeStatus() {
    const el = document.getElementById('edge-mode-status');
    if (!el) return;
    if (!addEdgeMode) {
      el.textContent = '未开启添加边模式';
      return;
    }
    if (!addEdgeFrom) {
      el.textContent = '添加边模式已开启：请点击起点节点';
      return;
    }
    el.textContent = '已选择起点：' + addEdgeFrom + '，请点击终点节点';
  }

  function loadAndRender() {
    fetch('/graph')
      .then((r) => r.json())
      .then((gj) => {
        const nodeList = gj.nodes || gj.Nodes || [];
        const edgeList = gj.edges || gj.Edges || [];
        fullNodes = nodeList;
        fullEdges = edgeList;

        // 不使用 x,y，让 vis 自动排布，保证一定能看到
        function rand() { return (Math.random() - 0.5) * 400; }
        const vsNodes = nodeList.map((n) => {
          const id = nodeIdOf(n);
          const pos = nodePosOf(n);
          const x = pos.x;
          const y = pos.y;
          const hasPos = x != null && y != null;
          const node = { id, label: id, x: hasPos ? x : rand(), y: hasPos ? y : rand() };
          return node;
        });
        var edgeIds = {};
        edgeList.forEach(function (e) {
          var ftc = edgeFromToCost(e);
          var from = ftc.from;
          var to = ftc.to;
          edgeIds[edgeId(from, to)] = true;
        });
        const vsEdges = edgeList.map((e) => {
          const ftc = edgeFromToCost(e);
          const from = ftc.from;
          const to = ftc.to;
          const w = ftc.cost;
          const id = edgeId(from, to);
          const revId = edgeId(to, from);
          const isBidi = edgeIds[revId];
          const edge = { id, from, to, label: String(w) };
          if (isBidi) {
            edge.smooth = id < revId ? false : { type: 'curvedCW', roundness: 0.4 };
          }
          return edge;
        });

        nodes.clear();
        edges.clear();
        nodes.add(vsNodes);
        edges.add(vsEdges);

        network = new vis.Network(container, { nodes, edges }, options);
        setEdgeModeStatus();

        // 拖动结束时，把新的坐标保存回后端（更新 data/graph.json）
        network.on('dragEnd', function (params) {
          if (params.nodes.length !== 1) return;
          const id = params.nodes[0];
          const pos = network.getPositions([id])[id];
          if (!pos) return;
          fetch('/save-position', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: id, x: pos.x, y: pos.y }),
          }).catch((e) => console.error('save-position failed', e));
        });

        // 点击：在添加边模式下优先处理节点点击；否则保持单击边改费用逻辑
        network.on('click', function (params) {
          if (addEdgeMode && params.nodes.length === 1) {
            const nid = params.nodes[0];
            if (!addEdgeFrom) {
              addEdgeFrom = nid;
              setEdgeModeStatus();
              return;
            }
            const from = addEdgeFrom;
            const to = nid;
            if (from === to) {
              alert('起点和终点不能相同');
              return;
            }
            const cost = parseInt((document.getElementById('add-edge-cost') || {}).value, 10);
            if (isNaN(cost) || cost < 1 || cost > 1000) {
              alert('费用必须在 1-1000 之间');
              return;
            }
            const des = (document.getElementById('add-edge-des') || {}).value || '';
            const typeNum = parseInt((document.getElementById('add-edge-type') || {}).value, 10);
            const statusNum = parseInt((document.getElementById('add-edge-status') || {}).value, 10);
            const payload = { from: from, to: to, cost: cost, des: des };
            if (!isNaN(typeNum)) payload.type = typeNum;
            if (!isNaN(statusNum)) payload.status = statusNum;
            fetch('/add-edge', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
            })
              .then(function (res) {
                if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
                fullEdges.push({ from: from, to: to, cost: cost, des: des, type: payload.type, status: payload.status });
                const id = edgeId(from, to);
                edges.add({ id: id, from: from, to: to, label: String(cost), smooth: edgeSmoothFor(from, to) });
                updateBidiStylesForPair(from, to);
                addEdgeFrom = null;
                setEdgeModeStatus();
              })
              .catch(function (e) { alert('添加边失败: ' + e.message); });
            return;
          }

          if (params.edges.length !== 1) return;
          const id = params.edges[0];
          const parsed = parseEdgeId(id);
          if (!parsed) return;
          if (edgeClickTimeout) clearTimeout(edgeClickTimeout);
          edgeClickTimeout = setTimeout(function () {
            edgeClickTimeout = null;
            const from = parsed.from, to = parsed.to;
            const edge = edges.get(id);
            const cur = edge && edge.label != null ? String(edge.label) : '';
            const input = prompt(from + ' → ' + to + ' 的费用 (1-1000):', cur);
            if (input === null) return;
            const cost = parseInt(input, 10);
            if (isNaN(cost) || cost < 1 || cost > 1000) {
              alert('费用必须在 1-1000 之间');
              return;
            }
            var eObj = fullEdges.find(function (e) {
              var ftc = edgeFromToCost(e);
              var f = ftc.from, t = ftc.to;
              return f === from && t === to;
            });
            var payload = { from: from, to: to, cost: cost };
            if (eObj) { payload.des = eObj.des != null ? eObj.des : eObj.Des || ''; if (eObj.type !== undefined && eObj.type !== null) payload.type = eObj.type; else if (eObj.Type !== undefined && eObj.Type !== null) payload.type = eObj.Type; if (eObj.status !== undefined && eObj.status !== null) payload.status = eObj.status; else if (eObj.Status !== undefined && eObj.Status !== null) payload.status = eObj.Status; }
            fetch('/update-edge', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
            })
              .then(function (res) {
                if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
                edges.update({ id: id, label: String(cost) });
                const eObj = fullEdges.find(function (e) {
                  var ftc = edgeFromToCost(e);
                  var f = ftc.from, t = ftc.to;
                  return f === from && t === to;
                });
                if (eObj) eObj.cost = cost;
              })
              .catch(function (e) { alert('更新失败: ' + e.message); });
          }, 250);
        });

        // 双击节点或边：打开详情弹窗，显示并可编辑 type / status / des
        network.on('doubleClick', function (params) {
          if (edgeClickTimeout) { clearTimeout(edgeClickTimeout); edgeClickTimeout = null; }
          if (params.nodes.length === 1) {
            openNodeDetailModal(params.nodes[0]);
            return;
          }
          if (params.edges.length === 1) {
            openEdgeDetailModal(params.edges[0]);
          }
        });

        requestAnimationFrame(function () { network.fit(); });
      })
      .catch((e) => {
        console.error('load /graph failed', e);
        container.textContent = '加载 data/graph.json 失败：' + e.message;
      });
  }

  function getNodeById(id) {
    return fullNodes.find(function (n) { return nodeIdOf(n) === id; });
  }
  function getEdgeByFromTo(from, to) {
    return fullEdges.find(function (e) {
      var ftc = edgeFromToCost(e);
      var f = ftc.from, t = ftc.to;
      return f === from && t === to;
    });
  }
  function val(obj, key, def) {
    if (obj == null) return def;
    var v = obj[key];
    if (v === undefined) v = obj[key === 'des' ? 'des' : key === 'type' ? 'type' : key === 'status' ? 'status' : key];
    return v !== undefined && v !== null ? String(v) : (def != null ? String(def) : '');
  }

  function openNodeDetailModal(nodeId) {
    var node = getNodeById(nodeId);
    if (!node) return;
    editContext = { type: 'node', id: nodeId };
    document.getElementById('detail-title').textContent = '节点详情';
    var des = val(node, 'des', '');
    var typeVal = (node.type !== undefined && node.type !== null) ? node.type : ((node.Type !== undefined && node.Type !== null) ? node.Type : '');
    var statusVal = (node.status !== undefined && node.status !== null) ? node.status : ((node.Status !== undefined && node.Status !== null) ? node.Status : '');
    document.getElementById('detail-fields').innerHTML =
      '<label>节点 ID</label><input type="text" id="detail-id" readonly value="' + escapeAttr(nodeId) + '">' +
      '<label>描述 (des)</label><input type="text" id="detail-des" value="' + escapeAttr(des) + '">' +
      '<label>类型 (type)</label><input type="number" id="detail-type" value="' + escapeAttr(typeVal) + '">' +
      '<label>状态 (status)</label><input type="number" id="detail-status" value="' + escapeAttr(statusVal) + '">';
    document.getElementById('detail-overlay').classList.add('show');
  }
  function escapeAttr(s) {
    if (s === undefined || s === null) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/"/g, '&quot;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }
  function openEdgeDetailModal(edgeId) {
    var parsed = parseEdgeId(edgeId);
    if (!parsed) return;
    var from = parsed.from, to = parsed.to;
    var edge = getEdgeByFromTo(from, to);
    if (!edge) return;
    editContext = { type: 'edge', from: from, to: to };
    var costVal = edge.cost != null ? edge.cost : edge.Cost;
    var des = val(edge, 'des', '');
    var typeVal = (edge.type !== undefined && edge.type !== null) ? edge.type : ((edge.Type !== undefined && edge.Type !== null) ? edge.Type : '');
    var statusVal = (edge.status !== undefined && edge.status !== null) ? edge.status : ((edge.Status !== undefined && edge.Status !== null) ? edge.Status : '');
    document.getElementById('detail-title').textContent = '边详情';
    document.getElementById('detail-fields').innerHTML =
      '<label>起点 (from)</label><input type="text" id="detail-from" readonly value="' + escapeAttr(from) + '">' +
      '<label>终点 (to)</label><input type="text" id="detail-to" readonly value="' + escapeAttr(to) + '">' +
      '<label>费用 (cost)</label><input type="number" id="detail-cost" min="1" max="1000" value="' + escapeAttr(costVal) + '">' +
      '<label>描述 (des)</label><input type="text" id="detail-des" value="' + escapeAttr(des) + '">' +
      '<label>类型 (type)</label><input type="number" id="detail-type" value="' + escapeAttr(typeVal) + '">' +
      '<label>状态 (status)</label><input type="number" id="detail-status" value="' + escapeAttr(statusVal) + '">';
    document.getElementById('detail-overlay').classList.add('show');
  }
  // parseEdgeId is defined above

  document.getElementById('detail-cancel').addEventListener('click', function () {
    document.getElementById('detail-overlay').classList.remove('show');
    editContext = null;
  });
  document.getElementById('detail-overlay').addEventListener('click', function (e) {
    if (e.target.id === 'detail-overlay') {
      document.getElementById('detail-overlay').classList.remove('show');
      editContext = null;
    }
  });
  document.getElementById('detail-save').addEventListener('click', function () {
    if (!editContext) return;
    if (editContext.type === 'node') {
      var id = document.getElementById('detail-id').value.trim();
      var des = document.getElementById('detail-des').value;
      var typeNum = parseInt(document.getElementById('detail-type').value, 10);
      var statusNum = parseInt(document.getElementById('detail-status').value, 10);
      if (id === '') { alert('节点 ID 不能为空'); return; }
      var payload = { id: id, des: des };
      if (!isNaN(typeNum)) payload.type = typeNum;
      if (!isNaN(statusNum)) payload.status = statusNum;
      fetch('/update-node', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
        .then(function (res) {
          if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
          var node = getNodeById(id);
          if (node) { node.des = des; if (!isNaN(typeNum)) node.type = typeNum; if (!isNaN(statusNum)) node.status = statusNum; }
          document.getElementById('detail-overlay').classList.remove('show');
          editContext = null;
        })
        .catch(function (e) { alert('更新失败: ' + e.message); });
      return;
    }
    if (editContext.type === 'edge') {
      var from = document.getElementById('detail-from').value.trim();
      var to = document.getElementById('detail-to').value.trim();
      var cost = parseInt(document.getElementById('detail-cost').value, 10);
      var des = document.getElementById('detail-des').value;
      var typeNum = parseInt(document.getElementById('detail-type').value, 10);
      var statusNum = parseInt(document.getElementById('detail-status').value, 10);
      if (from === '' || to === '') { alert('起点和终点不能为空'); return; }
      if (isNaN(cost) || cost < 1 || cost > 1000) { alert('费用必须在 1-1000 之间'); return; }
      var payload = { from: from, to: to, cost: cost, des: des };
      if (!isNaN(typeNum)) payload.type = typeNum;
      if (!isNaN(statusNum)) payload.status = statusNum;
      fetch('/update-edge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
        .then(function (res) {
          if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
          var edge = getEdgeByFromTo(from, to);
          if (edge) { edge.cost = cost; edge.des = des; if (!isNaN(typeNum)) edge.type = typeNum; if (!isNaN(statusNum)) edge.status = statusNum; }
          edges.update({ id: from + '->' + to, label: String(cost) });
          document.getElementById('detail-overlay').classList.remove('show');
          editContext = null;
        })
        .catch(function (e) { alert('更新失败: ' + e.message); });
    }
  });

  // Sidebar actions
  const btnAddNode = document.getElementById('btn-add-node');
  if (btnAddNode) {
    btnAddNode.addEventListener('click', function () {
      const idEl = document.getElementById('add-node-id');
      const id = idEl ? idEl.value.trim() : '';
      if (!id) { alert('节点 ID 不能为空'); return; }
      if (getNodeById(id)) { alert('节点已存在: ' + id); return; }

      const des = (document.getElementById('add-node-des') || {}).value || '';
      const typeNum = parseInt((document.getElementById('add-node-type') || {}).value, 10);
      const statusNum = parseInt((document.getElementById('add-node-status') || {}).value, 10);

      let x = (Math.random() - 0.5) * 200, y = (Math.random() - 0.5) * 200;
      if (network && typeof network.getViewPosition === 'function') {
        const vp = network.getViewPosition();
        x = vp.x + (Math.random() - 0.5) * 120;
        y = vp.y + (Math.random() - 0.5) * 120;
      }
      const payload = { id: id, x: x, y: y, des: des };
      if (!isNaN(typeNum)) payload.type = typeNum;
      if (!isNaN(statusNum)) payload.status = statusNum;

      fetch('/add-node', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
        .then(function (res) {
          if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
          fullNodes.push({ id: id, x: x, y: y, des: des, type: payload.type, status: payload.status });
          nodes.add({ id: id, label: id, x: x, y: y });
          if (idEl) idEl.value = '';
        })
        .catch(function (e) { alert('添加节点失败: ' + e.message); });
    });
  }

  const btnEdgeMode = document.getElementById('btn-edge-mode');
  if (btnEdgeMode) {
    btnEdgeMode.addEventListener('click', function () {
      addEdgeMode = !addEdgeMode;
      addEdgeFrom = null;
      btnEdgeMode.textContent = addEdgeMode ? '退出“添加边模式”' : '进入“添加边模式”';
      setEdgeModeStatus();
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', loadAndRender);
  } else {
    loadAndRender();
  }
})();

