// Indexes orval's generated operations by route, so the console's client keeps its
// path-keyed calls (api.GET("/calc-rules/{name}", { params: { path: { name } } }))
// on top of the orval layer (ADR-1619). orval emits one function per operation;
// this reads the same OpenAPI and writes, for each path and method, the operation's
// path, query, body and response types, each taken from that function's signature
// so the two can never disagree. Path and query params are the only ones mapped:
// orval drops header params, and the only one the spec has is logout's Cookie,
// which the browser sends itself.
import { readFileSync, writeFileSync } from "node:fs";
import { camel } from "orval";

const oas = JSON.parse(readFileSync(new URL("../../api/openapi.json", import.meta.url)));
const METHODS = ["get", "put", "post", "delete", "patch"];

// A path param's type: the spec types them as string or integer.
const scalar = (schema) => (schema?.type === "integer" || schema?.type === "number" ? "number" : schema?.type === "boolean" ? "boolean" : "string");

// orval's operation signature is (path params in route order, body?, query?, options?).
function operation(op) {
  const name = camel(op.operationId);
  const fn = `typeof og.${name}`;
  const params = op.parameters ?? [];
  const path = params.filter((p) => p.in === "path");
  const hasQuery = params.some((p) => p.in === "query");
  const hasBody = op.requestBody !== undefined;
  const arg = (i) => `NonNullable<Parameters<${fn}>[${i}]>`;
  const lines = [
    `path: ${path.length ? `{ ${path.map((p) => `${JSON.stringify(p.name)}: ${scalar(p.schema)}`).join("; ")} }` : "never"};`,
    `query: ${hasQuery ? arg(path.length + (hasBody ? 1 : 0)) : "never"};`,
    `body: ${hasBody ? arg(path.length) : "never"};`,
    `bodyRequired: ${hasBody && op.requestBody.required ? "true" : "false"};`,
    `data: og.${name}ResponseSuccess["data"];`,
    `error: og.${name}ResponseError["data"];`,
  ];
  return lines.map((l) => `      ${l}`).join("\n");
}

let out =
  "// Generated from api/openapi.json by web/scripts/gen-api-paths.mjs (make gen). Do not edit by hand.\n" +
  'import type * as og from "./api.gen";\n\n' +
  "export interface paths {\n";
for (const route of Object.keys(oas.paths).sort()) {
  out += `  ${JSON.stringify(route)}: {\n`;
  for (const method of METHODS) {
    const op = oas.paths[route][method];
    if (!op) continue;
    out += `    ${method}: {\n${operation(op)}\n    };\n`;
  }
  out += "  };\n";
}
out += "}\n";

writeFileSync(new URL("../src/api/paths.gen.ts", import.meta.url), out);
console.log("wrote web/src/api/paths.gen.ts");
