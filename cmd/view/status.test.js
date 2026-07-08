const assert = require('node:assert/strict');
const test = require('node:test');

const status = require('./static/status.js');

test('node status labels show availability', () => {
  assert.equal(status.NODE_STATUS_UNAVAILABLE, 0);
  assert.equal(status.NODE_STATUS_AVAILABLE, 1);
  assert.equal(status.nodeStatusLabel(0), '不可用');
  assert.equal(status.nodeStatusLabel(1), '可用');
  assert.equal(status.nodeStatusLabel(2), '不可用');
});

test('node status availability accepts only status 1', () => {
  assert.equal(status.isNodeAvailable(0), false);
  assert.equal(status.isNodeAvailable(1), true);
  assert.equal(status.isNodeAvailable('1'), true);
  assert.equal(status.isNodeAvailable(null), false);
});
