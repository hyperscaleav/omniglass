  const toast = document.getElementById('toast');
  function flash(msg, isErr) {
    toast.textContent = msg;
    toast.className = 'toast show' + (isErr ? ' err' : '');
    setTimeout(() => { toast.className = 'toast' + (isErr ? ' err' : ''); }, 1400);
  }
  // This page is served by the simulated device's own HTTP listener.
  // apiURL() resolves absolute "/api/..." paths against the page's own
  // path, so API calls stay relative to wherever the page is served.
  function apiURL(path) {
    if (!path.startsWith('/')) return path;
    const base = location.pathname.replace(/\/$/, '');
    return base + path;
  }
  async function send(path, body) {
    try {
      const r = await fetch(apiURL(path), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!r.ok) { flash('error: ' + r.status, true); return false; }
      flash('applied');
      return true;
    } catch (e) { flash('network error', true); return false; }
  }
  function reload() { setTimeout(() => location.reload(), 250); }

  document.querySelectorAll('.scene-btn').forEach(btn => {
    btn.onclick = async () => {
      if (await send('/api/room/scene', { scene: btn.dataset.scene })) reload();
    };
  });
  document.querySelectorAll('.source-btn').forEach(btn => {
    btn.onclick = async () => {
      if (await send('/api/room/source', { source: btn.dataset.source })) reload();
    };
  });
  const slider = document.getElementById('brightness');
  const sliderVal = document.getElementById('brightness-val');
  slider.oninput = () => { sliderVal.textContent = slider.value + '%'; };
  slider.onchange = async () => {
    await send('/api/panel/brightness', { brightness: Number(slider.value) });
  };

  // WEB-DRIVER-ONLY datapoint: decode the room config version. The
  // server embeds it as a base64 blob in data-cfgblob; nothing in the
  // served HTML carries it in plaintext, so a curl scrape never sees
  // the value. atob() runs only in a real browser (or a headless
  // JS-executing driver), which renders it into the DOM here.
  (() => {
    const el = document.getElementById('config-version');
    if (!el) return;
    const blob = el.dataset.cfgblob || '';
    try {
      el.textContent = blob ? atob(blob) : '(unset)';
    } catch (e) {
      el.textContent = '(decode error)';
    }
  })();
