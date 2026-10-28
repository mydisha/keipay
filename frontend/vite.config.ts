/// <reference types="vitest/config" />
import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

// The dev server proxies API paths to the Go backend. `make dev` and
// `make frontend` set VITE_API_TARGET from the API port in .env.
export default defineConfig(({ mode }) => {
  const env = { ...loadEnv(mode, process.cwd(), "VITE_"), ...process.env };
  const target = env.VITE_API_TARGET || "http://localhost:8080";
  const proxy = Object.fromEntries(
    ["/admin", "/v1", "/webhooks", "/healthz"].map((prefix) => [
      prefix,
      {
        target,
        changeOrigin: true,
        // `/webhooks` is also a console page: a browser navigation (reload,
        // deep link) gets the SPA, while API calls still reach the backend.
        bypass: (req: { method?: string; headers: { accept?: string } }) =>
          req.method === "GET" && req.headers.accept?.includes("text/html") ? "/index.html" : undefined,
      },
    ]),
  );

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    server: { port: 5173, proxy },
    test: {
      environment: "jsdom",
      globals: false,
      setupFiles: ["./src/test/setup.ts"],
      include: ["src/**/*.test.{ts,tsx}"],
      css: false,
      restoreMocks: true,
      unstubGlobals: true,
    },
  };
});
