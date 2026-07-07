(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
  root.RouteComposer = api;
})(typeof globalThis !== 'undefined' ? globalThis : this, function () {
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

  return {
    composeWaypointPaths: composeWaypointPaths,
    edgeId: edgeId,
  };
});
