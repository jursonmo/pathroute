(function (root, factory) {
  const statusApi = (typeof module === 'object' && module.exports)
    ? require('./status.js')
    : root.NodeStatus;
  const api = factory(statusApi);
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.NodeVisual = api;
  }
})(typeof window !== 'undefined' ? window : null, function (statusApi) {
  const nodeStatus = statusApi || {
    NODE_STATUS_UNAVAILABLE: 0,
    NODE_STATUS_AVAILABLE: 1,
    normalizeNodeStatus: function (value) { return Number(value) === 1 ? 1 : 0; },
    isNodeAvailable: function (value) { return Number(value) === 1; },
    nodeStatusLabel: function (value) { return Number(value) === 1 ? '可用' : '不可用'; },
    nodeStatusFontColor: function (value) { return Number(value) === 1 ? '#ffffff' : '#ff4d6d'; },
  };

  const DEFAULT_NODE_BG = '#0f3460';
  const DEFAULT_NODE_BORDER = '#e94560';
  const DEFAULT_NODE_FONT_COLOR = '#fff';
  const UNAVAILABLE_NODE_BG = '#4b5563';
  const UNAVAILABLE_NODE_BORDER = '#9ca3af';

  function nodeIdOf(n) {
    if (typeof n === 'string') return n;
    if (n == null) return '';
    return n.nodeId != null ? n.nodeId : n.nodeID || n.id || n.ID;
  }

  function nodePosOf(n) {
    if (typeof n !== 'object' || n == null) return { x: undefined, y: undefined };
    const x = n.x != null ? n.x : n.X;
    const y = n.y != null ? n.y : n.Y;
    return { x, y };
  }

  function rawNodeStatusOf(n) {
    if (typeof n !== 'object' || n == null) return nodeStatus.NODE_STATUS_UNAVAILABLE;
    const status = n.status != null ? n.status : n.Status;
    return nodeStatus.normalizeNodeStatus(status);
  }

  function escapeLabel(value) {
    if (value === undefined || value === null) return '';
    return String(value)
      .replace(/&/g, '&amp;')
      .replace(/"/g, '&quot;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  function nodeColorForStatus(status) {
    if (nodeStatus.isNodeAvailable(status)) {
      return { background: DEFAULT_NODE_BG, border: DEFAULT_NODE_BORDER };
    }
    return { background: UNAVAILABLE_NODE_BG, border: UNAVAILABLE_NODE_BORDER };
  }

  function nodeStatusFontFor(status) {
    return {
      size: 16,
      color: DEFAULT_NODE_FONT_COLOR,
      multi: 'html',
      mono: {
        color: nodeStatus.nodeStatusFontColor(status),
        size: 16,
        face: 'system-ui, sans-serif',
        vadjust: 0,
        mod: '',
      },
    };
  }

  function nodeVisualData(n) {
    const id = nodeIdOf(n);
    const pos = nodePosOf(n);
    const x = pos.x;
    const y = pos.y;
    const hasPos = x != null && y != null;
    const status = rawNodeStatusOf(n);
    const statusLabel = nodeStatus.nodeStatusLabel(status);
    const label = nodeStatus.isNodeAvailable(status)
      ? escapeLabel(id)
      : escapeLabel(id) + '\n<code>' + escapeLabel(statusLabel) + '</code>';
    return {
      id: id,
      label: label,
      title: '状态：' + statusLabel,
      x: hasPos ? x : undefined,
      y: hasPos ? y : undefined,
      color: nodeColorForStatus(status),
      font: nodeStatusFontFor(status),
    };
  }

  return {
    nodeIdOf,
    nodePosOf,
    rawNodeStatusOf,
    nodeColorForStatus,
    nodeStatusFontFor,
    nodeVisualData,
  };
});
