# Edge Status Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make only edges with `status === 1` routable, render unavailable edges red, and highlight route nodes and edges green.

**Architecture:** Add a browser-side `EdgeStatus` module as the single source of edge availability and color semantics. Use it in both the route composer and `cmd/view` rendering, while independently applying the same fail-closed rule in `internal/viewdb` before server-side graph calculation.

**Tech Stack:** Go, Node.js built-in test runner, browser JavaScript, vis-network, GORM-backed view store.

## Global Constraints

- Only edge `status === 1` is available.
- Edge status `0`, missing values, and every other value are unavailable.
- Unavailable edges stay visible and render red.
- Highlighted route nodes and edges render green.
- Do not change status semantics for unrelated consumers of the general-purpose `graph` package.

---

### Task 1: Edge status semantics and browser route filtering

**Files:**
- Create: `cmd/view/static/edge_status.js`
- Create: `cmd/view/edge_status.test.js`
- Modify: `cmd/view/static/route.js`
- Modify: `cmd/view/route.test.js`

**Interfaces:**
- Produces: `window.EdgeStatus` and CommonJS exports containing `EDGE_STATUS_UNAVAILABLE`, `EDGE_STATUS_AVAILABLE`, `EDGE_COLOR_AVAILABLE`, `EDGE_COLOR_UNAVAILABLE`, `ROUTE_HIGHLIGHT_COLOR`, `normalizeEdgeStatus(value)`, `rawEdgeStatusOf(edge)`, `isEdgeAvailable(value)`, and `edgeColorForStatus(value)`.
- Consumes: Edge objects using either lowercase JSON fields (`status`) or Go-style uppercase fields (`Status`).

- [ ] **Step 1: Write failing edge status tests**

```js
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
```

- [ ] **Step 2: Verify the new module test fails**

Run: `node --test cmd/view/edge_status.test.js`

Expected: FAIL with `Cannot find module './static/edge_status.js'`.

- [ ] **Step 3: Implement the edge status module**

```js
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  if (root) root.EdgeStatus = api;
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
    if (typeof edge !== 'object' || edge == null) return EDGE_STATUS_UNAVAILABLE;
    const value = edge.status != null ? edge.status : edge.Status;
    return normalizeEdgeStatus(value);
  }

  function isEdgeAvailable(value) {
    return normalizeEdgeStatus(value) === EDGE_STATUS_AVAILABLE;
  }

  function edgeColorForStatus(value) {
    return isEdgeAvailable(value) ? EDGE_COLOR_AVAILABLE : EDGE_COLOR_UNAVAILABLE;
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
  };
});
```

- [ ] **Step 4: Verify edge status tests pass**

Run: `node --test cmd/view/edge_status.test.js`

Expected: PASS, 3 tests.

- [ ] **Step 5: Write a failing route test and make existing route fixtures explicitly available**

Add `status: 1` to every edge fixture used by `findOrderedSimplePaths`, then add:

```js
test('findOrderedSimplePaths excludes unavailable edges', () => {
  const edges = [
    { from: 'A', to: 'B', cost: 1, status: 0 },
    { from: 'A', to: 'C', cost: 2, status: 1 },
    { from: 'C', to: 'B', cost: 2, status: 1 },
  ];

  const got = findOrderedSimplePaths(edges, ['A', 'B'], 4);

  assert.equal(got.reachable, true);
  assert.deepEqual(got.paths[0], { path: ['A', 'C', 'B'], distance: 4 });
});
```

- [ ] **Step 6: Verify the route test fails for the expected reason**

Run: `node --test cmd/view/route.test.js`

Expected: FAIL because the current route is `A -> B` with distance `1`.

- [ ] **Step 7: Inject EdgeStatus into the route composer and filter adjacency input**

Change the UMD wrapper to pass `require('./edge_status.js')` under Node and `root.EdgeStatus` in the browser. In `buildAdjacency`, skip an edge before parsing it when `rawEdgeStatusOf(edge)` is not available:

```js
if (!edgeStatus || !edgeStatus.isEdgeAvailable(edgeStatus.rawEdgeStatusOf(edge))) return;
```

- [ ] **Step 8: Verify route and status tests pass**

Run: `node --test cmd/view/edge_status.test.js cmd/view/route.test.js`

Expected: PASS.

### Task 2: View rendering, editing, and green route highlights

**Files:**
- Modify: `cmd/view/static/index.html`
- Modify: `cmd/view/static/app.js`

**Interfaces:**
- Consumes: `window.EdgeStatus` from Task 1.
- Produces: Red unavailable edges on load/add/edit, blue available edges, green route highlights, and normalized 0/1 edge status form values.

- [ ] **Step 1: Load EdgeStatus before its consumers and replace the add-edge number input**

Load scripts in this order:

```html
<script src="edge_status.js"></script>
<script src="route.js"></script>
<script src="status.js"></script>
<script src="node_visual.js"></script>
<script src="app.js"></script>
```

Replace `add-edge-status` with:

```html
<label>是否可用</label>
<select id="add-edge-status">
  <option value="0" selected>不可用</option>
  <option value="1">可用</option>
</select>
```

- [ ] **Step 2: Add EdgeStatus adapters to app.js**

Create an `edgeStatus` fallback matching Task 1, plus:

```js
function rawEdgeStatusOf(edge) {
  return edgeStatus.rawEdgeStatusOf(edge);
}

function isEdgeAvailable(edge) {
  return edgeStatus.isEdgeAvailable(rawEdgeStatusOf(edge));
}

function edgeColorForEdge(edge) {
  return { color: edgeStatus.edgeColorForStatus(rawEdgeStatusOf(edge)) };
}

function edgeStatusOptions(selected) {
  const status = edgeStatus.normalizeEdgeStatus(selected);
  return '<option value="0"' + (status === edgeStatus.EDGE_STATUS_UNAVAILABLE ? ' selected' : '') + '>不可用</option>'
    + '<option value="1"' + (status === edgeStatus.EDGE_STATUS_AVAILABLE ? ' selected' : '') + '>可用</option>';
}
```

- [ ] **Step 3: Apply status colors and filter route inputs**

Include `isEdgeAvailable(e)` in `routeEdgesForCalculation`. Add `color: edgeColorForEdge(e)` when building initial vis-network edges and newly added edges. Normalize the add form with `normalizeEdgeStatus`, always send `payload.status`, and store the normalized status in `fullEdges`.

- [ ] **Step 4: Restore status colors and use green highlights**

When clearing highlighted edges, find the matching object in `fullEdges` and restore `edgeColorForEdge(edge)`. Change both highlight updates to use `edgeStatus.ROUTE_HIGHLIGHT_COLOR`:

```js
return { id, color: { background: edgeStatus.ROUTE_HIGHLIGHT_COLOR, border: '#ffffff' } };
```

```js
return { id, color: { color: edgeStatus.ROUTE_HIGHLIGHT_COLOR }, width: 4 };
```

- [ ] **Step 5: Normalize edge detail editing and invalidate stale results**

Render the edge detail status as `<select>` using `edgeStatusOptions(statusVal)`. On save, normalize and always submit the status. After success, update the edge object, set `shortestResults = null`, clear highlights, and update the vis edge label and status-derived color.

- [ ] **Step 6: Run browser-side regression tests**

Run: `node --test cmd/view/*.test.js`

Expected: PASS with no failures.

### Task 3: Server-side edge filtering

**Files:**
- Modify: `internal/viewdb/store.go`
- Modify: `internal/viewdb/store_test.go`

**Interfaces:**
- Produces: `isEdgeAvailable(status int) bool` and a `buildAvailableGraphJSON` result containing only available nodes connected by available edges.
- Consumes: Existing `GraphDTO`, `NodeDTO`, and `EdgeDTO` values.

- [ ] **Step 1: Write failing Go tests**

Add table coverage for edge statuses:

```go
func TestIsEdgeAvailable(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		expected bool
	}{
		{name: "unavailable status", status: edgeStatusUnavailable, expected: false},
		{name: "available status", status: edgeStatusAvailable, expected: true},
		{name: "unknown status", status: 2, expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEdgeAvailable(tt.status); got != tt.expected {
				t.Fatalf("isEdgeAvailable(%d) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}
```

Make the existing `B -> C` fixture explicitly available, and add an unavailable `B -> D` edge between available nodes. Assert that only `B -> C` remains.

- [ ] **Step 2: Verify Go tests fail for the expected reason**

Run: `go test ./internal/viewdb`

Expected: build failure because `edgeStatusUnavailable`, `edgeStatusAvailable`, and `isEdgeAvailable` do not exist.

- [ ] **Step 3: Implement fail-closed edge filtering**

Add:

```go
const (
	edgeStatusUnavailable = 0
	edgeStatusAvailable   = 1
)

func isEdgeAvailable(status int) bool {
	return status == edgeStatusAvailable
}
```

In the edge loop of `buildAvailableGraphJSON`, add:

```go
if !isEdgeAvailable(e.Status) {
	continue
}
```

before appending the graph edge.

- [ ] **Step 4: Format and verify the package**

Run: `gofmt -w internal/viewdb/store.go internal/viewdb/store_test.go`

Run: `go test ./internal/viewdb`

Expected: PASS.

### Task 4: Full verification

**Files:**
- Verify all modified and created files from Tasks 1–3.

**Interfaces:**
- Consumes: Completed frontend and server implementations.
- Produces: Evidence that the repository remains green and the requested behavior is fully covered.

- [ ] **Step 1: Run all JavaScript tests**

Run: `node --test cmd/view/*.test.js`

Expected: PASS.

- [ ] **Step 2: Run all Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Check formatting and review the exact diff**

Run: `git diff --check`

Expected: no output.

Run: `git diff -- cmd/view internal/viewdb docs/superpowers/plans/2026-07-15-edge-status-routing.md`

Expected: only the edge status, route color, tests, and implementation-plan changes described above.
