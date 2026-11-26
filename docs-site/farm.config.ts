import { defineConfig } from "@farm.js/core";
import { devtools } from "@farm.js/devtools";

export default defineConfig({
  plugins: [devtools()],
  telemetry: false,
  theme: {
    default: "dark",
  },
  docs: {
    enabled: true,
  },
});
