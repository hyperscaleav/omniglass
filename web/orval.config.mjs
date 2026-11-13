// orval generates the console's typed API layer from the committed OpenAPI spec
// (ADR-1619): every schema as a TypeScript type, and one typed function per
// operation, all sending through the client's ogFetch so they share its base URL,
// session cookie, bearer token and 401 bounce. Schemas are type aliases rather
// than interfaces so a body still widens to a generic record (an interface has no
// implicit index signature). Prettier is off so the output depends only on the
// spec and the orval version, which keeps gen-drift exact.
export default {
  omniglass: {
    input: { target: "../api/openapi.json" },
    output: {
      target: "src/api/api.gen.ts",
      client: "fetch",
      mode: "single",
      prettier: false,
      override: { mutator: { path: "src/api/client.ts", name: "ogFetch" }, useTypeOverInterfaces: true },
    },
  },
};
