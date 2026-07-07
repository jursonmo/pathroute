const assert = require('node:assert/strict');
const test = require('node:test');

const { composeWaypointPaths } = require('./static/route.js');

function result(from, to, paths) {
  return {
    from,
    to,
    paths: paths.map(([path, distance]) => ({ path, distance })),
  };
}

function buildResults(items) {
  const m = new Map();
  for (const item of items) {
    m.set(item.from + '->' + item.to, item);
  }
  return m;
}

test('composeWaypointPaths returns top four combined paths in required stop order', () => {
  const results = buildResults([
    result('A', 'B', [
      [['A', 'X', 'B'], 4],
      [['A', 'B'], 5],
    ]),
    result('B', 'C', [
      [['B', 'C'], 1],
      [['B', 'Y', 'C'], 3],
    ]),
  ]);

  const got = composeWaypointPaths(results, ['A', 'B', 'C'], 4);

  assert.equal(got.reachable, true);
  assert.deepEqual(got.paths, [
    { path: ['A', 'X', 'B', 'C'], distance: 5 },
    { path: ['A', 'B', 'C'], distance: 6 },
    { path: ['A', 'X', 'B', 'Y', 'C'], distance: 7 },
    { path: ['A', 'B', 'Y', 'C'], distance: 8 },
  ]);
  assert.equal(got.failedSegment, null);
});

test('composeWaypointPaths stops at the first unreachable segment and returns reachable prefix', () => {
  const results = buildResults([
    result('A', 'B', [
      [['A', 'B'], 2],
      [['A', 'X', 'B'], 5],
    ]),
    result('B', 'C', []),
    result('C', 'D', [
      [['C', 'D'], 1],
    ]),
  ]);

  const got = composeWaypointPaths(results, ['A', 'B', 'C', 'D'], 4);

  assert.equal(got.reachable, false);
  assert.deepEqual(got.paths, []);
  assert.deepEqual(got.prefixPath, ['A', 'B']);
  assert.equal(got.prefixDistance, 2);
  assert.deepEqual(got.failedSegment, { from: 'B', to: 'C', index: 1 });
});
