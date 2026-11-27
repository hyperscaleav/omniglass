
  // Browser inherits the Basic auth credentials from the page load, so
  // these fetch calls do not need explicit Authorization headers. The
  // same-origin same-realm rule covers it.
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
    } catch (e) {
      flash('network error', true);
      return false;
    }
  }
  function reload() { setTimeout(() => location.reload(), 250); }

  document.getElementById('power-on').onclick = async () => {
    if (await send('/api/display/power', { power: 'on' })) reload();
  };
  document.getElementById('power-off').onclick = async () => {
    if (await send('/api/display/power', { power: 'off' })) reload();
  };
  document.getElementById('input-select').onchange = async (e) => {
    if (await send('/api/display/input', { input: e.target.value })) reload();
  };
  const bSlider = document.getElementById('brightness');
  const bVal = document.getElementById('brightness-val');
  bSlider.oninput = () => { bVal.textContent = bSlider.value + '%'; };
  bSlider.onchange = async () => {
    await send('/api/display/brightness', { brightness: Number(bSlider.value) });
  };
  const vSlider = document.getElementById('volume');
  const vVal = document.getElementById('volume-val');
  vSlider.oninput = () => { vVal.textContent = vSlider.value + '%'; };
  vSlider.onchange = async () => {
    await send('/api/audio/volume', { volume: Number(vSlider.value) });
  };
  const muteBtn = document.getElementById('mute-toggle');
  muteBtn.onclick = async () => {
    const muted = muteBtn.classList.contains('primary');
    if (await send('/api/audio/mute', { mute: !muted })) reload();
  };
