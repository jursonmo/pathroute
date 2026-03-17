(function () {
  // 最简单版本：读取 /graph，自动用 vis-network 画出节点和边，不做任何编辑
  const container = document.getElementById('graph');
  if (!container) return;

  const nodes = new vis.DataSet([]);
  const edges = new vis.DataSet([]);

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

  function loadAndRender() {
    fetch('/graph')
      .then((r) => r.json())
      .then((gj) => {
        const nodeList = gj.nodes || gj.Nodes || [];
        const edgeList = gj.edges || gj.Edges || [];

        // 不使用 x,y，让 vis 自动排布，保证一定能看到
        function rand() { return (Math.random() - 0.5) * 400; }
        const vsNodes = nodeList.map((n) => {
          const id = typeof n === 'string' ? n : n.id;
          const x = typeof n === 'object' ? n.x : undefined;
          const y = typeof n === 'object' ? n.y : undefined;
          const hasPos = x != null && y != null;
          const node = { id, label: id, x: hasPos ? x : rand(), y: hasPos ? y : rand() };
          return node;
        });
        var edgeIds = {};
        edgeList.forEach(function (e) {
          var from = e.from != null ? e.from : e.From;
          var to = e.to != null ? e.to : e.To;
          edgeIds[edgeId(from, to)] = true;
        });
        const vsEdges = edgeList.map((e) => {
          const from = e.from != null ? e.from : e.From;
          const to = e.to != null ? e.to : e.To;
          const w = e.weight != null ? e.weight : e.Weight;
          const id = edgeId(from, to);
          const revId = edgeId(to, from);
          const isBidi = edgeIds[revId];
          const edge = { id, from, to, label: String(w) };
          if (isBidi) {
            edge.smooth = id < revId ? false : { type: 'curvedCW', roundness: 0.4 };
          }
          return edge;
        });

        // 边 id 解析为 from, to（与 edgeId(from,to) 一致）
        function parseEdgeId(id) {
          const i = id.indexOf('->');
          if (i === -1) return null;
          return { from: id.slice(0, i), to: id.slice(i + 2) };
        }

        nodes.clear();
        edges.clear();
        nodes.add(vsNodes);
        edges.add(vsEdges);

        const network = new vis.Network(container, { nodes, edges }, options);

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

        // 点击边（含 weight 标签）时，弹出输入框修改该边的 weight，双向边按方向分别修改
        network.on('click', function (params) {
          if (params.edges.length !== 1) return;
          const id = params.edges[0];
          const parsed = parseEdgeId(id);
          if (!parsed) return;
          const from = parsed.from, to = parsed.to;
          const edge = edges.get(id);
          const cur = edge && edge.label != null ? String(edge.label) : '';
          const input = prompt(from + ' → ' + to + ' 的权值 (1-1000):', cur);
          if (input === null) return;
          const weight = parseInt(input, 10);
          if (isNaN(weight) || weight < 1 || weight > 1000) {
            alert('权值必须在 1-1000 之间');
            return;
          }
          fetch('/update-edge', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ from: from, to: to, weight: weight }),
          })
            .then(function (res) {
              if (!res.ok) return res.text().then(function (t) { throw new Error(t); });
              edges.update({ id: id, label: String(weight) });
            })
            .catch(function (e) {
              alert('更新失败: ' + e.message);
            });
        });

        requestAnimationFrame(function () { network.fit(); });
      })
      .catch((e) => {
        console.error('load /graph failed', e);
        container.textContent = '加载 data/graph.json 失败：' + e.message;
      });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', loadAndRender);
  } else {
    loadAndRender();
  }
})();

