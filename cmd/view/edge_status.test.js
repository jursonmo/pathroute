const assert = require('node:assert/strict');
const test = require('node:test');

const edgeStatus = require('./static/edge_status.js');

test('only edge status 1 is available', () => {
  assert.equal(edgeStatus.isEdgeAvailable(0), false);
  assert.equal(edgeStatus.isEdgeAvailable(1), true);
  assert.equal(edgeStatus.isEdgeAvailable('1'), true);
  assert.equal(edgeStatus.isEdgeAvailable(2), false);
  assert.equal(edgeStatus.isEdgeAvailable(undefined), false);
});

test('edge colors distinguish unavailable edges from highlighted routes', () => {
  assert.equal(edgeStatus.edgeColorForStatus(0), '#ff4d6d');
  assert.equal(edgeStatus.edgeColorForStatus(1), '#4a9eff');
  assert.equal(edgeStatus.ROUTE_HIGHLIGHT_COLOR, '#2ecc71');
});

test('raw edge status accepts lowercase and uppercase fields', () => {
  assert.equal(edgeStatus.rawEdgeStatusOf({ status: 1 }), 1);
  assert.equal(edgeStatus.rawEdgeStatusOf({ Status: 1 }), 1);
  assert.equal(edgeStatus.rawEdgeStatusOf({}), 0);
});

test('edge visual data and status options follow normalized status', () => {
  assert.deepEqual(edgeStatus.normalEdgeVisual({ status: 0 }), {
    color: { color: '#ff4d6d' },
  });
  assert.deepEqual(edgeStatus.normalEdgeVisual({ status: 1 }), {
    color: { color: '#4a9eff' },
  });
  assert.match(edgeStatus.edgeStatusOptions(0), /value="0" selected/);
  assert.match(edgeStatus.edgeStatusOptions(1), /value="1" selected/);
});

test('route highlight visual data is green for nodes and edges', () => {
  assert.deepEqual(edgeStatus.routeNodeHighlight('A'), {
    id: 'A',
    color: { background: '#2ecc71', border: '#ffffff' },
  });
  assert.deepEqual(edgeStatus.routeEdgeHighlight('A->B'), {
    id: 'A->B',
    color: { color: '#2ecc71' },
    width: 4,
  });
});
