import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";

const backend = process.env.VITE_BACKEND ?? "http://localhost:8080";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      manifest: {
        name: "Projector Remote",
        short_name: "Remote",
        description: "Low-latency remote control for the living room projector",
        theme_color: "#f97316",
        background_color: "#09090b",
        display: "standalone",
        icons: [
          { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
        ],
      },
    }),
  ],
  server: {
    proxy: {
      "/api": backend,
      "/ws": {
        target: backend,
        ws: true,
      },
    },
  },
});
