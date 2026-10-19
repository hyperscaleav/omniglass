// The walkthrough stack's readiness wait and its proof, run against the real
// stack (never mocked) in a container on the stack network:
//
//   node e2e/walkthrough-stack.mjs ready   wait until the node collects from the simulated fleet
//   node e2e/walkthrough-stack.mjs check   ready, then prove the simulator holds what impairments
//                                          need, and that an impairment set through the API
//                                          applies (reachability goes down) and repair recovers it
//
// Env: OG_E2E_BASE (the server, on the stack network) and OG_TOKEN.

const mode = process.argv[2] ?? 'check';
const base = process.env.OG_E2E_BASE;
const token = process.env.OG_TOKEN;
if (!base || !token || !['ready', 'check'].includes(mode)) {
  console.error('usage: OG_E2E_BASE=... OG_TOKEN=... node e2e/walkthrough-stack.mjs ready|check');
  process.exit(2);
}

async function api(method, path, body) {
  const res = await fetch(`${base}/api/v1${path}`, {
    method,
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}

// waitFor polls fn until it returns a truthy value, or fails after limitMs.
async function waitFor(what, fn, limitMs = 120_000) {
  const start = Date.now();
  let last;
  for (;;) {
    try {
      last = await fn();
      if (last) {
        console.log(`ok: ${what} (${((Date.now() - start) / 1000).toFixed(1)}s)`);
        return last;
      }
    } catch (err) {
      last = err;
    }
    if (Date.now() - start > limitMs) {
      const why = last instanceof Error ? last.message : JSON.stringify(last);
      throw new Error(`timed out after ${limitMs / 1000}s waiting for ${what} (last: ${why})`);
    }
    await new Promise((r) => setTimeout(r, 2000));
  }
}

// A component is addressed by its id: simulated components share names
// (every room has a display-1), and the API takes an id where a name would
// be ambiguous.
async function verdicts(id) {
  const r = await api('GET', `/components/${id}/reachability`);
  return r.endpoints.filter((e) => e.verdict).map((e) => `${e.endpoint}=${e.verdict.value}`);
}
const allAre = (v, want) => v.length > 0 && v.every((s) => s.endsWith(`=${want}`));

const sim = await waitFor('the simulator beside edge-hq is connected and serving', async () => {
  const s = await api('GET', '/nodes/edge-hq/simulation');
  return s.connected && s.devices.some((d) => d.status === 'running') ? s : null;
});

// The first running component whose every verdicted endpoint reads up: the
// node is collecting from it. (A component collected only through a driver,
// SNMP say, carries no reachability verdict and is passed over.)
const device = await waitFor('a simulated component to read up (the node is collecting)', async () => {
  for (const d of sim.devices.filter((d) => d.status === 'running')) {
    if (allAre(await verdicts(d.component_id), 'up')) return d;
  }
  return null;
});
const label = `${device.component} (${device.product}, ${device.address})`;
console.log(`   ${label}: ${(await verdicts(device.component_id)).join(' ')}`);
if (mode === 'ready') process.exit(0);

if (!sim.capabilities.net_admin || !sim.impairments.available) {
  throw new Error(`the simulator cannot apply impairments: ${JSON.stringify(sim.impairments)}`);
}
if (sim.capabilities.port_offset !== 0) {
  throw new Error(`the simulator lifts its ports by ${sim.capabilities.port_offset}; in a container it binds the real ones`);
}
console.log(`ok: the simulator serves ${sim.devices.length} components with NET_ADMIN and no port lift`);

await api('PUT', `/components/${device.component_id}/impairment`, { unplugged: true });
console.log(`   unplugged ${label} through the API`);
await waitFor('it to read down once unplugged', async () => allAre(await verdicts(device.component_id), 'down'));
console.log(`   ${(await verdicts(device.component_id)).join(' ')}`);

await api('PUT', `/components/${device.component_id}/impairment`, {});
console.log(`   repaired ${label} through the API`);
await waitFor('it to read up once repaired', async () => allAre(await verdicts(device.component_id), 'up'));
console.log(`   ${(await verdicts(device.component_id)).join(' ')}`);

console.log('walkthrough stack: collecting, an impairment applies, repair recovers');
