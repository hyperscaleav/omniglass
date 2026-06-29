import { defineConfig } from "vitest/config";
import solid from "vite-plugin-solid";

// jsdom so component tests can render; the data-layer tests need only the
// happy-dom-free environment for fetch mocking.
export default defineConfig({
  plugins: [solid()],
  test: {
    environment: "jsdom",
    globals: true,
  },
  resolve: { conditions: ["development", "browser"] },
});
