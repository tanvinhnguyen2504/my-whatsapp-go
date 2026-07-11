import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go server runs on :8082. Proxying keeps the browser same-origin, so the
// WebSocket same-origin check passes and no CORS setup is needed.
const backend = "http://localhost:8082";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/ws": { target: backend, ws: true },
      "/messages": { target: backend },
    },
  },
});
