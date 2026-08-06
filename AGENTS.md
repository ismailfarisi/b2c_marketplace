# Project Notes & Memory

## Docker Compose & Internet Exposure (Cloudflare Tunnel)

- **Docker Compose Services**:
  - `postgres` (PostGIS 16), `redis`, `minio` (S3 object storage), `minio-init`, `mailpit`.
  - `api`: Go backend API (`services/api/Dockerfile`).
  - `web`: Next.js web app (`apps/web/Dockerfile`).
  - `gateway`: Nginx reverse proxy on port 80 (`deploy/nginx/nginx.conf`). Routes `/v1/*` & `/healthz` to `api:8080`, and `/*` to `web:3000`.
  - `tunnel`: Cloudflare Tunnel (`cloudflare/cloudflared:latest`) running quick tunnel on `http://gateway:80`.

- **Commands**:
  - Start full stack: `docker compose up -d`
  - Get live public HTTPS tunnel URL: `docker compose logs tunnel`
  - Build Android APK pointing to Cloudflare Tunnel URL:
    `powershell -ExecutionPolicy Bypass -File scripts/build-apk-with-tunnel.ps1`
  - Output APK path: `apps/mobile-kmm/androidApp/build/outputs/apk/debug/androidApp-debug.apk`
