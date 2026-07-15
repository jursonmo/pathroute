(function (root, factory) {
  const edgeStatus = (typeof module === 'object' && module.exports)
    ? require('./edge_status.js')
    : root.EdgeStatus;
  const api = factory(edgeStatus);
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  root.RouteComposer = api;
})(typeof globalThis !== 'undefined' ? globalThis : this, function (edgeStatus) {
  function edgeId(from, to) {
    return from + '->' + to;
  }

  function pathKey(path) {
    return (path || []).join('|');
  }

  function resultFor(results, from, to) {
    if (!results) return null;
    const key = edgeId(from, to);
    if (typeof results.get === 'function') return results.get(key) || null;
    return results[key] || null;
  }

  function validPaths(pr, maxPaths) {
    if (!pr || !Array.isArray(pr.paths)) return [];
    return pr.paths
      .filter(function (p) {
        return p && Array.isArray(p.path) && p.path.length > 0 && typeof p.distance === 'number';
      })
      .slice(0, maxPaths);
  }

  function joinPaths(left, right) {
    const a = Array.isArray(left) ? left : [];
    const b = Array.isArray(right) ? right : [];
    if (a.length === 0) return b.slice();
    if (b.length === 0) return a.slice();
    if (a[a.length - 1] === b[0]) return a.concat(b.slice(1));
    return a.concat(b);
  }

  function takeBest(paths, maxPaths) {
    const sorted = paths.slice().sort(function (a, b) {
      if (a.distance !== b.distance) return a.distance - b.distance;
      return pathKey(a.path).localeCompare(pathKey(b.path));
    });
    const out = [];
    const seen = {};
    for (let i = 0; i < sorted.length && out.length < maxPaths; i++) {
      const key = pathKey(sorted[i].path);
      if (seen[key]) continue;
      seen[key] = true;
      out.push(sorted[i]);
    }
    return out;
  }

  function combinePathSets(prefixes, segmentPaths, maxPaths) {
    const candidates = [];
    for (let i = 0; i < prefixes.length; i++) {
      for (let j = 0; j < segmentPaths.length; j++) {
        candidates.push({
          path: joinPaths(prefixes[i].path, segmentPaths[j].path),
          distance: prefixes[i].distance + segmentPaths[j].distance,
        });
      }
    }
    return takeBest(candidates, maxPaths);
  }

  function cleanStops(stops) {
    return (stops || []).filter(function (stop) {
      return stop !== undefined && stop !== null && String(stop).trim() !== '';
    }).map(function (stop) {
      return String(stop);
    });
  }

  function hasDuplicate(items) {
    const seen = {};
    for (let i = 0; i < items.length; i++) {
      if (seen[items[i]]) return true;
      seen[items[i]] = true;
    }
    return false;
  }

  function edgeFromToCost(e) {
    if (!e) return { from: '', to: '', cost: 0 };
    const from = e.from != null ? e.from : e.From;
    const to = e.to != null ? e.to : e.To;
    const cost = e.cost != null ? e.cost : e.Cost;
    return { from: String(from || ''), to: String(to || ''), cost: Number(cost) };
  }

  function buildAdjacency(edges) {
    const adj = {};
    (edges || []).forEach(function (edge) {
      if (!edgeStatus || !edgeStatus.isEdgeAvailable(edgeStatus.rawEdgeStatusOf(edge))) return;
      const ftc = edgeFromToCost(edge);
      if (!ftc.from || !ftc.to || !(ftc.cost > 0)) return;
      if (!adj[ftc.from]) adj[ftc.from] = [];
      adj[ftc.from].push({ to: ftc.to, cost: ftc.cost });
    });
    Object.keys(adj).forEach(function (from) {
      adj[from].sort(function (a, b) {
        if (a.cost !== b.cost) return a.cost - b.cost;
        return a.to.localeCompare(b.to);
      });
    });
    return adj;
  }

  function stopIndexMap(stops) {
    const out = {};
    for (let i = 0; i < stops.length; i++) out[stops[i]] = i;
    return out;
  }

  function betterPrefix(a, b) {
    if (!a) return b;
    if (!b) return a;
    if (b.distance !== a.distance) return b.distance < a.distance ? b : a;
    return pathKey(b.path).localeCompare(pathKey(a.path)) < 0 ? b : a;
  }

  function pushState(queue, state) {
    queue.push(state);
    queue.sort(function (a, b) {
      if (a.distance !== b.distance) return a.distance - b.distance;
      return pathKey(a.path).localeCompare(pathKey(b.path));
    });
  }

  function composeWaypointPaths(results, stops, maxPaths) {
    const limit = maxPaths > 0 ? maxPaths : 4;
    const orderedStops = cleanStops(stops);
    if (orderedStops.length < 2) {
      const start = orderedStops.length === 1 ? [orderedStops[0]] : [];
      return {
        reachable: false,
        paths: [],
        prefixPath: start,
        prefixDistance: 0,
        failedSegment: null,
      };
    }

    let combinations = [{ path: [orderedStops[0]], distance: 0 }];
    let prefix = combinations[0];

    for (let i = 0; i + 1 < orderedStops.length; i++) {
      const from = orderedStops[i];
      const to = orderedStops[i + 1];
      const segmentPaths = validPaths(resultFor(results, from, to), limit);
      if (segmentPaths.length === 0) {
        return {
          reachable: false,
          paths: [],
          prefixPath: prefix.path,
          prefixDistance: prefix.distance,
          failedSegment: { from: from, to: to, index: i },
        };
      }
      combinations = combinePathSets(combinations, segmentPaths, limit);
      prefix = combinations[0];
    }

    return {
      reachable: true,
      paths: combinations,
      prefixPath: prefix.path,
      prefixDistance: prefix.distance,
      failedSegment: null,
    };
  }

  function findOrderedSimplePaths(edges, stops, maxPaths) {
    const limit = maxPaths > 0 ? maxPaths : 4;
    const orderedStops = cleanStops(stops);
    const start = orderedStops.length > 0 ? orderedStops[0] : '';
    if (orderedStops.length < 2) {
      return {
        reachable: false,
        paths: [],
        prefixPath: start ? [start] : [],
        prefixDistance: 0,
        failedSegment: null,
      };
    }
    if (hasDuplicate(orderedStops)) {
      return {
        reachable: false,
        paths: [],
        prefixPath: start ? [start] : [],
        prefixDistance: 0,
        failedSegment: null,
        invalidReason: '起点/中途点/终点不能重复',
      };
    }

    const adj = buildAdjacency(edges);
    const requiredIndex = stopIndexMap(orderedStops);
    const targetIndex = orderedStops.length - 1;
    const queue = [];
    const results = [];
    const bestPrefixByStopIndex = [];
    bestPrefixByStopIndex[0] = { path: [start], distance: 0 };

    pushState(queue, {
      node: start,
      stopIndex: 0,
      path: [start],
      distance: 0,
      visited: Object.assign({}, { [start]: true }),
    });

    while (queue.length > 0 && results.length < limit) {
      const cur = queue.shift();
      if (cur.stopIndex === targetIndex) {
        results.push({ path: cur.path, distance: cur.distance });
        continue;
      }

      const nextRequired = orderedStops[cur.stopIndex + 1];
      const outgoing = adj[cur.node] || [];
      for (let i = 0; i < outgoing.length; i++) {
        const edge = outgoing[i];
        if (cur.visited[edge.to]) continue;

        const requiredToIndex = requiredIndex[edge.to];
        if (requiredToIndex !== undefined && requiredToIndex > cur.stopIndex + 1) {
          continue;
        }

        let nextStopIndex = cur.stopIndex;
        if (edge.to === nextRequired) {
          nextStopIndex = cur.stopIndex + 1;
        }

        const nextPath = cur.path.concat(edge.to);
        const nextDistance = cur.distance + edge.cost;
        const nextVisited = Object.assign({}, cur.visited);
        nextVisited[edge.to] = true;
        const nextState = {
          node: edge.to,
          stopIndex: nextStopIndex,
          path: nextPath,
          distance: nextDistance,
          visited: nextVisited,
        };

        if (nextStopIndex > cur.stopIndex) {
          bestPrefixByStopIndex[nextStopIndex] = betterPrefix(bestPrefixByStopIndex[nextStopIndex], {
            path: nextPath,
            distance: nextDistance,
          });
        }
        pushState(queue, nextState);
      }
    }

    if (results.length > 0) {
      return {
        reachable: true,
        paths: takeBest(results, limit),
        prefixPath: results[0].path,
        prefixDistance: results[0].distance,
        failedSegment: null,
      };
    }

    let bestIndex = 0;
    for (let i = 1; i < bestPrefixByStopIndex.length; i++) {
      if (bestPrefixByStopIndex[i]) bestIndex = i;
    }
    const bestPrefix = bestPrefixByStopIndex[bestIndex] || { path: start ? [start] : [], distance: 0 };
    const failedFrom = orderedStops[bestIndex];
    const failedTo = orderedStops[bestIndex + 1];
    return {
      reachable: false,
      paths: [],
      prefixPath: bestPrefix.path,
      prefixDistance: bestPrefix.distance,
      failedSegment: failedTo ? { from: failedFrom, to: failedTo, index: bestIndex } : null,
    };
  }

  return {
    composeWaypointPaths: composeWaypointPaths,
    findOrderedSimplePaths: findOrderedSimplePaths,
    edgeId: edgeId,
  };
});
