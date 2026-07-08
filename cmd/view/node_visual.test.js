const assert = require('node:assert/strict');
const test = require('node:test');

const status = require('./static/status.js');
const visual = require('./static/node_visual.js');

test('unavailable node status label uses red status font', () => {
  const node = visual.nodeVisualData({ nodeId: 'G', status: status.NODE_STATUS_UNAVAILABLE });

  assert.equal(node.label, 'G\n<code>不可用</code>');
  assert.equal(node.font.multi, 'html');
  assert.equal(node.font.color, '#fff');
  assert.equal(node.font.mono.color, status.NODE_STATUS_UNAVAILABLE_LABEL_COLOR);
});

test('available node status label keeps the normal status font', () => {
  const node = visual.nodeVisualData({ nodeId: 'A', status: status.NODE_STATUS_AVAILABLE });

  assert.equal(node.label, 'A\n<code>可用</code>');
  assert.equal(node.font.multi, 'html');
  assert.equal(node.font.mono.color, status.NODE_STATUS_AVAILABLE_LABEL_COLOR);
});
