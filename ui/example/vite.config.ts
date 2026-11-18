import { vitePlugin as remix } from "@remix-run/dev";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig({
  build: {
    target: ["chrome107", "edge107", "firefox104", "safari16"],
  },
  plugins: [
    tailwindcss(),
    remix({
      ssr: true,
    }),
  ],
  resolve: {
    alias: {
      "~": resolve(__dirname, "./app"),
      // Point to library source for hot-reload during development
      "@datum-cloud/activity-ui": resolve(__dirname, "../src/index.ts"),
    },
  },
  ssr: {
    // Bundle the library for SSR instead of externalizing it
    noExternal: ["@datum-cloud/activity-ui"],
  },
  optimizeDeps: {
    esbuildOptions: {
      target: ["chrome107", "edge107", "firefox104", "safari16"],
    },
    exclude: ["@remix-run/react", "monaco-editor"],
  },
  worker: {
    format: 'es',
  },
  server: {
    host: '0.0.0.0',
    port: 3000,
  },
});
