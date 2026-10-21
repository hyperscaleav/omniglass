// The digest reducer's command (#965): read a probe run's raw events and
// write the digest an agent reads, then print a one-line summary.
//
//   node web/e2e/digest.mjs <raw.jsonl> <digest.json>
import { readFile, writeFile } from 'node:fs/promises';
import { parseRaw, reduce, serialize } from './digestlib.mjs';

const [raw, out] = process.argv.slice(2);
if (!raw || !out) {
  console.error('usage: node web/e2e/digest.mjs <raw.jsonl> <digest.json>');
  process.exit(2);
}
// Routes are grouped by the API's own path templates, read from the
// generated OpenAPI document rather than restated here.
const openapi = JSON.parse(await readFile(new URL('../../api/openapi.json', import.meta.url), 'utf8'));
const prefix = openapi.servers?.[0]?.url ?? '';
const digest = reduce(parseRaw(await readFile(raw, 'utf8')), {}, Object.keys(openapi.paths).map((p) => prefix + p));
await writeFile(out, serialize(digest));
const bySeverity = { high: 0, medium: 0, low: 0 };
for (const f of digest.findings) bySeverity[f.severity]++;
console.log(
  `digest: ${digest.surfaces} surfaces, ${digest.requests} API calls, ` +
    `${digest.findings.length} findings (${bySeverity.high} high, ${bySeverity.medium} medium, ${bySeverity.low} low)` +
    (digest.truncated ? `, ${digest.truncated} more dropped by the cap` : '') +
    ` -> ${out}`,
);
