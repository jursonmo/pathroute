(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.NodeStatus = api;
  }
})(typeof window !== 'undefined' ? window : null, function () {
  const NODE_STATUS_UNAVAILABLE = 0;
  const NODE_STATUS_AVAILABLE = 1;
  const NODE_STATUS_UNAVAILABLE_LABEL_COLOR = '#ff4d6d';
  const NODE_STATUS_AVAILABLE_LABEL_COLOR = '#ffffff';

  function normalizeNodeStatus(value) {
    return Number(value) === NODE_STATUS_AVAILABLE ? NODE_STATUS_AVAILABLE : NODE_STATUS_UNAVAILABLE;
  }

  function isNodeAvailable(value) {
    return normalizeNodeStatus(value) === NODE_STATUS_AVAILABLE;
  }

  function nodeStatusLabel(value) {
    return isNodeAvailable(value) ? '可用' : '不可用';
  }

  function nodeStatusFontColor(value) {
    return isNodeAvailable(value) ? NODE_STATUS_AVAILABLE_LABEL_COLOR : NODE_STATUS_UNAVAILABLE_LABEL_COLOR;
  }

  return {
    NODE_STATUS_UNAVAILABLE,
    NODE_STATUS_AVAILABLE,
    NODE_STATUS_UNAVAILABLE_LABEL_COLOR,
    NODE_STATUS_AVAILABLE_LABEL_COLOR,
    normalizeNodeStatus,
    isNodeAvailable,
    nodeStatusLabel,
    nodeStatusFontColor,
  };
});
