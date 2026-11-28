
  // Browser carries the Basic auth credentials over to fetch() calls
  // automatically because they are same-origin same-realm with the
  // page load that just succeeded.
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
        body: JSON.stringify(body || {}),
      });
      if (!r.ok) { flash('error: ' + r.status, true); return false; }
      flash('applied');
      return true;
    } catch (e) { flash('network error', true); return false; }
  }
  function reload() { setTimeout(() => location.reload(), 250); }

  const muteBtn = document.getElementById('mute-toggle');
  if (muteBtn) muteBtn.onclick = async () => {
    const muted = muteBtn.classList.contains('danger');
    if (await send('/api/audio/mute', { mute: !muted })) reload();
  };
  const camBtn = document.getElementById('cam-toggle');
  if (camBtn) camBtn.onclick = async () => {
    const on = camBtn.classList.contains('primary');
    if (await send('/api/video/camera', { camera_on: !on })) reload();
  };
  const slider = document.getElementById('speaker');
  const sliderVal = document.getElementById('speaker-val');
  if (slider) {
    slider.oninput = () => { sliderVal.textContent = slider.value; };
    slider.onchange = async () => {
      await send('/api/audio/volume', { volume: Number(slider.value) });
    };
  }
  const standbyOn = document.getElementById('standby-on');
  if (standbyOn) standbyOn.onclick = async () => {
    if (await send('/api/system/standby', { power: 'standby' })) reload();
  };
  const standbyOff = document.getElementById('standby-off');
  if (standbyOff) standbyOff.onclick = async () => {
    if (await send('/api/system/standby', { power: 'on' })) reload();
  };
  const hangup = document.getElementById('hangup');
  if (hangup) hangup.onclick = async () => {
    if (await send('/api/call/disconnect', {})) reload();
  };

  // Recent logs panel. Fetches the PRIVATE GET /api/logs route, the
  // same undocumented endpoint a student finds in the network tab and
  // can then call directly (e.g. as a collector's HTTP item). The page's
  // Basic-auth creds ride along on this same-origin GET automatically.
  const logsEl = document.getElementById('logs');
  async function loadLogs() {
    if (!logsEl) return;
    try {
      const r = await fetch(apiURL('/api/logs'));
      if (!r.ok) { logsEl.innerHTML = '<li class="muted">logs unavailable (' + r.status + ')</li>'; return; }
      const data = await r.json();
      const lines = (data && data.recent) || [];
      if (!lines.length) { logsEl.innerHTML = '<li class="muted">no recent log lines</li>'; return; }
      logsEl.innerHTML = '';
      for (const line of lines) {
        const li = document.createElement('li');
        li.textContent = line;
        logsEl.appendChild(li);
      }
    } catch (e) {
      logsEl.innerHTML = '<li class="muted">network error loading logs</li>';
    }
  }
  loadLogs();
