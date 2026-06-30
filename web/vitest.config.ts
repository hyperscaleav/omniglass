import { defineConfig, configDefaults } from "vitest/config";
import solid from "vite-plugin-solid";

// jsdom so component tests can render; the data-layer tests need only the
// happy-dom-free environment for fetch mocking. The browser e2e (Playwright) lives
// under e2e/ and is excluded here; it runs via `make test-e2e`.
export default defineConfig({
  plugins: [solid()],
  test: {
    environment: "jsdom",
    globals: true,
    exclude: [...configDefaults.exclude, "e2e/**"],
  },
  resolve: { conditions: ["development", "browser"] },
});
