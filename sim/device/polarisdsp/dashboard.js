  const HEADER = 'X-Polaris-Token';
  const KEY = 'polaris-token-__DEVICE_ID__';
  const tokenInput = document.getElementById('token-input');
  const tokenHint = document.getElementById('token-hint');
  const authCard = document.getElementById('auth-card');
  const toast = document.getElementById('toast');

  function flash(msg, isErr) {
    toast.textContent = msg;
    toast.className = 'toast show' + (isErr ? ' err' : '');
    setTimeout(() => { toast.className = 'toast' + (isErr ? ' err' : ''); }, 1400);
  }
  function getToken() { return localStorage.getItem(KEY) || ''; }
  function setToken(v) { if (v) localStorage.setItem(KEY, v); else localStorage.removeItem(KEY); paint(); }
  function paint() {
    const t = getToken();
    tokenInput.value = t;
    if (t) {
      authCard.classList.add('ok');
      tokenHint.textContent = 'Token saved. Controls active.';
      document.querySelectorAll('button.mute, input[type=range][data-role=gain]')
        .forEach(el => { el.disabled = false; });
    } else {
      authCard.classList.remove('ok');
      tokenHint.textContent = 'No token saved, controls disabled.';
      document.querySelectorAll('button.mute, input[type=range][data-role=gain]')
        .forEach(el => { el.disabled = true; });
    }
  }

  document.getElementById('token-save').onclick = () => {
    setToken(tokenInput.value.trim());
    flash('token saved');
  };

  // This page is served by the simulated device's own HTTP listener.
  // apiURL() resolves absolute "/api/..." paths against the page's own
  // path, so API calls stay relative to wherever the page is served.
  function apiURL(path) {
    if (!path.startsWith('/')) return path;
    const base = location.pathname.replace(/\/$/, '');
    return base + path;
  }
  async function send(path, body) {
    const t = getToken();
    if (!t) { flash('save a token first', true); return false; }
    try {
      const r = await fetch(apiURL(path), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', [HEADER]: t },
        body: JSON.stringify(body),
      });
      if (r.status === 401) { flash('401: token rejected', true); return false; }
      if (!r.ok) { flash('error: ' + r.status, true); return false; }
      flash('applied');
      return true;
    } catch (e) {
      flash('network error', true);
      return false;
    }
  }
  function reload() { setTimeout(() => location.reload(), 250); }

  document.querySelectorAll('tr[data-idx]').forEach(row => {
    const idx = Number(row.dataset.idx);
    const muteBtn = row.querySelector('button[data-role=mute]');
    muteBtn.onclick = async () => {
      const muted = muteBtn.classList.contains('muted');
      if (await send('/api/channels/mute', { index: idx, mute: !muted })) reload();
    };
    const gain = row.querySelector('input[data-role=gain]');
    const gainVal = row.querySelector('[data-role=gain-val]');
    gain.oninput = () => { gainVal.textContent = gain.value; };
    gain.onchange = async () => {
      await send('/api/channels/gain', { index: idx, gain: Number(gain.value) });
    };
  });

  paint();
