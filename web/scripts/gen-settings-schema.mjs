// Slices the settings namespace field constraints out of the generated OpenAPI
// into a client artifact the settings form validates against, so the form's rules
// come from the same reflected source as the server (no hand-kept second copy).
import { readFileSync, writeFileSync } from "node:fs";

const oas = JSON.parse(readFileSync(new URL("../../api/openapi.json", import.meta.url)));
const schemas = oas.components.schemas;

const deref = (s) => (s && s.$ref ? schemas[s.$ref.split("/").pop()] : s);

// Find the Settings schema: the `values` property of the settings read body.
const body = schemas.SettingsReadOutputBody;
const settingsSchema = deref(body.properties.values);

const keep = ["type", "enum", "pattern", "minLength", "maxLength", "minimum", "maximum", "format"];
const constraint = (propSchema) => {
  const s = deref(propSchema);
  const c = {};
  for (const k of keep) if (s[k] !== undefined) c[k] = s[k];
  return c;
};

const out = {};
for (const [ns, nsRef] of Object.entries(settingsSchema.properties)) {
  const nsSchema = deref(nsRef);
  out[ns] = {};
  for (const [key, propSchema] of Object.entries(nsSchema.properties || {})) {
    out[ns][key] = constraint(propSchema);
  }
}

const banner =
  "// Generated from api/openapi.json by web/scripts/gen-settings-schema.mjs (make gen). Do not edit by hand.\n";
writeFileSync(
  new URL("../src/api/settings.schema.gen.ts", import.meta.url),
  banner + "export const settingsSchema = " + JSON.stringify(out, null, 2) + " as const;\n"
);
console.log("wrote web/src/api/settings.schema.gen.ts");
