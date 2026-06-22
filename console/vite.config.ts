import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

const API_TARGET = process.env.VITE_API_TARGET || "http://localhost:8090";
const AUTH_TARGET = process.env.VITE_AUTH_TARGET || "http://localhost:8788";

// Shared proxy config. cookieDomainRewrite makes Set-Cookie headers from
// any non-localhost API target (staging / prod) land on localhost so
// browser-side auth works through the dev proxy.
const proxyOpts = {
  target: API_TARGET,
  changeOrigin: true,
  secure: true,
  cookieDomainRewrite: "localhost",
};

const authProxyOpts = {
  target: AUTH_TARGET,
  changeOrigin: true,
  secure: true,
  cookieDomainRewrite: "localhost",
};

// Mock /auth-info endpoint when no Go backend is running.
// Returns empty providers (no Google, no email-otp) and no Turnstile key.
function mockAuthInfoPlugin(): Plugin {
  return {
    name: "mock-auth-info",
    configureServer(server) {
      server.middlewares.use("/auth-info", (_req, res) => {
        res.setHeader("Content-Type", "application/json");
        res.end(JSON.stringify({ providers: [], turnstile_site_key: null }));
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), mockAuthInfoPlugin()],
  resolve: {
    alias: {
      "@/registry/default/ui": path.resolve(__dirname, "./src/components/ui"),
      "@/registry/default/blocks": path.resolve(__dirname, "./src/components/blocks"),
      "@/registry/default/hooks": path.resolve(__dirname, "./src/hooks"),
      "@/registry/default/lib": path.resolve(__dirname, "./src/lib"),
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": proxyOpts,
      "/v1": proxyOpts,
      "/auth": authProxyOpts,
      "/health": proxyOpts,
      "/linear": proxyOpts,
      "/linear-setup": proxyOpts,
      "/github": proxyOpts,
      "/github-setup": proxyOpts,
    },
  },
});
