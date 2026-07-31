import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    // Views reach Wails only through src/bindings.ts, so a view test stubs that
    // module rather than depending on the generated bindings, which exist only
    // after `wails build`. Nothing here drives a real desktop app: that would
    // test the harness rather than the code.
    include: ["src/**/*.test.ts"],
  },
});
