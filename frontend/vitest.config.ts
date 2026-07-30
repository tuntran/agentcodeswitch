import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    // Only components are covered. Views import the Wails-generated bindings, which
    // exist only after `wails build`, and driving a real desktop app from vitest
    // would test the harness rather than the code.
    include: ["src/**/*.test.ts"],
  },
});
