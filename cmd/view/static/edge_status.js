(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.EdgeStatus = api;
  }
})(typeof window !== 'undefined' ? window : null, function () {
  const EDGE_STATUS_UNAVAILABLE = 0;
  const EDGE_STATUS_AVAILABLE = 1;
  const EDGE_COLOR_AVAILABLE = '#4a9eff';
  const EDGE_COLOR_UNAVAILABLE = '#ff4d6d';
  const ROUTE_HIGHLIGHT_COLOR = '#2ecc71';

  function normalizeEdgeStatus(value) {
    return Number(value) === EDGE_STATUS_AVAILABLE
      ? EDGE_STATUS_AVAILABLE
      : EDGE_STATUS_UNAVAILABLE;
  }

  function rawEdgeStatusOf(edge) {
    if (typeof edge !== 'object' || edge == null) {
      return EDGE_STATUS_UNAVAILABLE;
    }
    const value = edge.status != null ? edge.status : edge.Status;
    return normalizeEdgeStatus(value);
  }

  function isEdgeAvailable(value) {
    return normalizeEdgeStatus(value) === EDGE_STATUS_AVAILABLE;
  }

  function edgeColorForStatus(value) {
    return isEdgeAvailable(value) ? EDGE_COLOR_AVAILABLE : EDGE_COLOR_UNAVAILABLE;
  }

  function normalEdgeVisual(edge) {
    return {
      color: { color: edgeColorForStatus(rawEdgeStatusOf(edge)) },
    };
  }

  function edgeStatusOptions(selected) {
    const status = normalizeEdgeStatus(selected);
    return '<option value="0"' + (status === EDGE_STATUS_UNAVAILABLE ? ' selected' : '') + '>不可用</option>'
      + '<option value="1"' + (status === EDGE_STATUS_AVAILABLE ? ' selected' : '') + '>可用</option>';
  }

  function routeNodeHighlight(id) {
    return {
      id,
      color: { background: ROUTE_HIGHLIGHT_COLOR, border: '#ffffff' },
    };
  }

  function routeEdgeHighlight(id) {
    return {
      id,
      color: { color: ROUTE_HIGHLIGHT_COLOR },
      width: 4,
    };
  }

  return {
    EDGE_STATUS_UNAVAILABLE,
    EDGE_STATUS_AVAILABLE,
    EDGE_COLOR_AVAILABLE,
    EDGE_COLOR_UNAVAILABLE,
    ROUTE_HIGHLIGHT_COLOR,
    normalizeEdgeStatus,
    rawEdgeStatusOf,
    isEdgeAvailable,
    edgeColorForStatus,
    normalEdgeVisual,
    edgeStatusOptions,
    routeNodeHighlight,
    routeEdgeHighlight,
  };
});
