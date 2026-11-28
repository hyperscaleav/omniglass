
  // This page is served by the simulated device's own HTTP listener.
  // apiURL() resolves absolute "/api/..." paths against the page's own
  // path, so any future fetch stays relative to wherever the page is
  // served. (This dashboard is read-only today;
  // the helper is present for convention + future controls.)
  function apiURL(path) {
    if (!path.startsWith('/')) return path;
    const base = location.pathname.replace(/\/$/, '');
    return base + path;
  }
