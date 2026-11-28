
  // apiURL() resolves absolute "/api/..." paths against the page's own
  // path, the convention every device page keeps (read-only today).
  function apiURL(path) {
    if (!path.startsWith('/')) return path;
    const base = location.pathname.replace(/\/$/, '');
    return base + path;
  }
