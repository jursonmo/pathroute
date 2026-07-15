# Edge Status Routing Design

## Goal

Give edge `status` a consistent meaning in `cmd/view`:

- `status === 1` means available.
- `status === 0`, a missing status, and every other value mean unavailable.
- Only available edges participate in route calculations.
- Unavailable edges remain visible and are rendered red.
- A highlighted route renders all of its nodes and edges green so it is visually distinct from unavailable edges.

## Scope

The change covers the `cmd/view` browser UI and the graph assembled by `internal/viewdb` for the view server. It does not change status semantics for unrelated consumers of the general-purpose `graph` package.

## Design

### Status and visual behavior

Add a small, testable browser-side edge helper that normalizes an edge status, reports availability, and supplies the normal edge color. Available edges use the existing blue color; unavailable edges use red.

Initial graph rendering, adding an edge, and editing an edge all use this helper. Clearing a route highlight restores each edge to the color implied by its current status instead of restoring every edge to blue.

The existing route highlight changes from red to green for both nodes and edges. Clearing the highlight restores nodes using their node status and edges using their edge status.

### Editing

Replace free-form edge status number inputs in the add-edge panel and edge-detail dialog with two choices: unavailable (`0`) and available (`1`). New edges default to unavailable, matching the persisted default.

After an edge status update succeeds, update the in-memory edge data and visual style immediately, clear stale highlights, and invalidate cached route results so the next route calculation uses the new topology.

### Route calculation

The browser-side route input excludes any edge whose own status is not `1`, in addition to the existing exclusion of edges connected to unavailable nodes.

The server-side `internal/viewdb` graph builder applies the same rule before invoking the graph algorithms. This keeps `/calculate` and browser-composed waypoint routes consistent.

Unavailable edges stay in the graph DTO returned for visualization; they are removed only from calculation inputs.

## Tests

- JavaScript tests verify that only edge status `1` is available and that unavailable edges use red.
- Route tests verify that an unavailable edge is never selected, including when it would otherwise be cheaper.
- Go tests verify that the view database graph builder excludes unavailable edges while preserving available edges between available nodes.
- Existing Go and JavaScript test suites must remain green.

## Error handling and compatibility

Missing or malformed edge status values fail closed as unavailable. Existing records with `status = 1` retain their current behavior. Existing unavailable records stay editable and visible rather than disappearing from the visualization.
