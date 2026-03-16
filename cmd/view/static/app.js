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
      smooth: false,
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
        const vsEdges = edgeList.map((e) => {
          const from = e.from != null ? e.from : e.From;
          const to = e.to != null ? e.to : e.To;
          const w = e.weight != null ? e.weight : e.Weight;
          return { id: edgeId(from, to), from, to, label: String(w) };
        });

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

