
  // This page is served by the simulated device's own HTTP listener.
  // apiURL() resolves absolute "/api/..." paths against the page's own
  // path, so API calls stay relative to wherever the page is served.
  function apiURL(path) {
    if (!path.startsWith('/')) return path;
    const base = location.pathname.replace(/\/$/, '');
    return base + path;
  }
  // Poll the live occupancy feed so the count tile updates on its own.
  // The REST API is key-gated, so an unkeyed poll just no-ops quietly;
  // a full reload still reflects state on every page open.
  async function refresh() {
    try {
      const r = await fetch(apiURL('/api/occupancy'), {
        headers: { 'X-Census-Key': 'census-lab-key' },
      });
      if (!r.ok) return;
      const d = await r.json();
      const c = document.querySelector('.count');
      if (c) c.firstChild.nodeValue = String(d.people_count);
    } catch (e) { /* transient, keep polling */ }
  }
  setInterval(refresh, 5000);
