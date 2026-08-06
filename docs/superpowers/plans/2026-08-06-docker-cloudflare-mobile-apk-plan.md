# Docker Compose Stack with Cloudflare Tunnel & Mobile APK Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a complete Docker Compose environment running Postgres, Redis, MinIO, Go API, Next.js Web, and a Cloudflare Tunnel reverse proxy, alongside a script to build the Android APK pointed at the live public HTTPS tunnel endpoint.

**Architecture:** A unified Docker Compose stack orchestrates backend, database, web, and an Nginx reverse proxy gateway. `cloudflared` exposes gateway port 80 to an external HTTPS URL. The Android app (`mobile-kmm`) configuration is updated to accept `API_BASE_URL` dynamically from Gradle properties.

**Tech Stack:** Docker, Docker Compose, Cloudflare Tunnel (`cloudflared`), Nginx, Go 1.26, Next.js 15, Kotlin Multiplatform (Android Gradle).

## Global Constraints

- Preserve all existing environment variables in `.env.example`.
- Retain existing services in `docker-compose.yml` (`postgres`, `redis`, `minio`, `minio-init`, `mailpit`).
- All web paths `/*` route to Next.js (`web:3000`); `/v1/*` and `/healthz` route to Go API (`api:8080`).

---

### Task 1: Create Next.js Web Dockerfile

**Files:**
- Create: `apps/web/Dockerfile`

**Interfaces:**
- Consumes: Monorepo root files (`package.json`, `package-lock.json`), `apps/web` package files.
- Produces: Docker image `b2c-marketplace-web` listening on port 3000.

- [ ] **Step 1: Write `apps/web/Dockerfile`**

```dockerfile
FROM node:22-alpine AS base
WORKDIR /app

FROM base AS deps
RUN apk add --no-cache libc6-compat
COPY package.json package-lock.json ./
COPY apps/web/package.json ./apps/web/package.json
RUN npm ci

FROM base AS builder
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED 1
ENV NODE_ENV production
RUN npm run build:web

FROM base AS runner
ENV NODE_ENV production
ENV NEXT_TELEMETRY_DISABLED 1
ENV PORT 3000
ENV HOSTNAME "0.0.0.0"

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

COPY --from=builder /app/apps/web/public ./apps/web/public
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/apps/web/.next/static ./apps/web/.next/static

USER nextjs

EXPOSE 3000

CMD ["node", "apps/web/server.js"]
```

- [ ] **Step 2: Commit Task 1**

```bash
git add apps/web/Dockerfile
git commit -m "feat(web): add multi-stage Dockerfile for Next.js app"
```

---

### Task 2: Create Gateway Reverse Proxy Configuration

**Files:**
- Create: `deploy/nginx/nginx.conf`

**Interfaces:**
- Consumes: External HTTP traffic on port 80.
- Produces: Proxied requests to `api:8080` (for `/v1/` & `/healthz`) and `web:3000` (for all other paths).

- [ ] **Step 1: Write `deploy/nginx/nginx.conf`**

```nginx
events {
    worker_connections 1024;
}

http {
    include       mime.types;
    default_type  application/octet-stream;
    sendfile        on;
    keepalive_timeout  65;

    upstream api_backend {
        server api:8080;
    }

    upstream web_frontend {
        server web:3000;
    }

    server {
        listen 80;
        server_name localhost;

        client_max_body_size 100M;

        location /v1/ {
            proxy_pass http://api_backend/v1/;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }

        location /healthz {
            proxy_pass http://api_backend/healthz;
            proxy_set_header Host $host;
        }

        location / {
            proxy_pass http://web_frontend/;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }
    }
}
```

- [ ] **Step 2: Commit Task 2**

```bash
git add deploy/nginx/nginx.conf
git commit -m "feat(gateway): add Nginx reverse proxy configuration for API and Web"
```

---

### Task 3: Update `docker-compose.yml` with API, Web, Gateway, and Tunnel Services

**Files:**
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `services/api/Dockerfile`, `apps/web/Dockerfile`, `deploy/nginx/nginx.conf`.
- Produces: Fully wired Docker Compose environment with public HTTPS Cloudflare Tunnel output.

- [ ] **Step 1: Update `docker-compose.yml`**

Add `api`, `web`, `gateway`, and `tunnel` services to `docker-compose.yml`.

```yaml
  api:
    build:
      context: .
      dockerfile: services/api/Dockerfile
      args:
        CMD: api
    environment:
      DATABASE_URL: "postgres://marketplace:marketplace@postgres:5432/marketplace?sslmode=disable"
      REDIS_URL: "redis://redis:6379"
      S3_ENDPOINT: "http://minio:9000"
      S3_ACCESS_KEY: "minioadmin"
      S3_SECRET_KEY: "minioadmin"
      S3_BUCKET: "marketplace-media"
    ports:
      - "8080:8080"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      minio:
        condition: service_healthy

  web:
    build:
      context: .
      dockerfile: apps/web/Dockerfile
    ports:
      - "3000:3000"
    environment:
      NODE_ENV: production
    depends_on:
      - api

  gateway:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./deploy/nginx/nginx.conf:/etc/nginx/nginx.conf:ro
    depends_on:
      - api
      - web

  tunnel:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run --url http://gateway:80
    depends_on:
      - gateway
```

- [ ] **Step 2: Commit Task 3**

```bash
git add docker-compose.yml
git commit -m "feat(docker): add api, web, gateway, and cloudflared tunnel services to docker-compose"
```

---

### Task 4: Make Android App API Base URL Dynamic in Gradle

**Files:**
- Modify: `apps/mobile-kmm/androidApp/build.gradle.kts:18-21`

**Interfaces:**
- Consumes: Gradle parameter `-PAPI_BASE_URL="..."` or env var `API_BASE_URL`.
- Produces: `BuildConfig.API_BASE_URL` compiled into the Android APK.

- [ ] **Step 1: Update `apps/mobile-kmm/androidApp/build.gradle.kts`**

Replace lines 18-21 in `apps/mobile-kmm/androidApp/build.gradle.kts`:

```kotlin
        // Pointed at the Go API backend. Supports -PAPI_BASE_URL override for Cloudflare Tunnels / remote hosts.
        val apiBaseUrl = project.findProperty("API_BASE_URL") as? String
            ?: System.getenv("API_BASE_URL")
            ?: "http://10.0.2.2:8080/v1"

        buildConfigField("String", "API_BASE_URL", "\"$apiBaseUrl\"")
```

- [ ] **Step 2: Commit Task 4**

```bash
git add apps/mobile-kmm/androidApp/build.gradle.kts
git commit -m "feat(mobile): support dynamic API_BASE_URL in androidApp build config"
```

---

### Task 5: Create APK Build Script & Makefile Target

**Files:**
- Create: `scripts/build-apk-with-tunnel.ps1`
- Create: `scripts/build-apk-with-tunnel.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: Running `tunnel` container logs in Docker Compose.
- Produces: Android Debug APK compiled with Cloudflare HTTPS endpoint URL.

- [ ] **Step 1: Write `scripts/build-apk-with-tunnel.ps1`**

```powershell
$ErrorActionPreference = "Stop"

Write-Host "Extracting Cloudflare Tunnel URL from Docker logs..."
$logs = docker compose logs tunnel 2>&1
$match = $logs | Select-String -Pattern 'https://[a-zA-Z0-9-]+\.trycloudflare\.com' | Select-Object -Last 1

if (-not $match) {
    Write-Error "Could not find active Cloudflare Tunnel URL. Ensure 'docker compose up -d' is running."
    exit 1
}

$tunnelUrl = $match.Matches[0].Value
$apiBaseUrl = "$tunnelUrl/v1"

Write-Host "Found Cloudflare Tunnel URL: $tunnelUrl"
Write-Host "Building Android APK pointing to API_BASE_URL=$apiBaseUrl ..."

Push-Location "apps/mobile-kmm"
try {
    ./gradlew :androidApp:assembleDebug "-PAPI_BASE_URL=$apiBaseUrl"
} finally {
    Pop-Location
}

Write-Host "APK Build successful! Path: apps/mobile-kmm/androidApp/build/outputs/apk/debug/androidApp-debug.apk"
```

- [ ] **Step 2: Write `scripts/build-apk-with-tunnel.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Extracting Cloudflare Tunnel URL from Docker logs..."
TUNNEL_URL=$(docker compose logs tunnel 2>&1 | grep -oE 'https://[a-zA-Z0-9-]+\.trycloudflare\.com' | tail -n 1)

if [ -z "$TUNNEL_URL" ]; then
  echo "Error: Could not find active Cloudflare Tunnel URL. Ensure 'docker compose up -d' is running."
  exit 1
fi

API_BASE_URL="${TUNNEL_URL}/v1"
echo "Found Cloudflare Tunnel URL: $TUNNEL_URL"
echo "Building Android APK pointing to API_BASE_URL=$API_BASE_URL ..."

cd apps/mobile-kmm
./gradlew :androidApp:assembleDebug -PAPI_BASE_URL="$API_BASE_URL"

echo "APK Build successful! Path: apps/mobile-kmm/androidApp/build/outputs/apk/debug/androidApp-debug.apk"
```

- [ ] **Step 3: Update `Makefile`**

Add target:

```makefile
.PHONY: dev-apk-tunnel
dev-apk-tunnel: ## Build Android APK pointing to live Cloudflare Tunnel URL
	powershell -ExecutionPolicy Bypass -File scripts/build-apk-with-tunnel.ps1
```

- [ ] **Step 4: Commit Task 5**

```bash
git add scripts/build-apk-with-tunnel.ps1 scripts/build-apk-with-tunnel.sh Makefile
git commit -m "feat(mobile): add helper script and Makefile target for building tunnel-enabled APK"
```

---

### Task 6: End-to-End Verification

- [ ] **Step 1: Test Docker Compose Startup**

Run: `docker compose up -d`
Expected: Containers `postgres`, `redis`, `minio`, `minio-init`, `mailpit`, `api`, `web`, `gateway`, and `tunnel` are running.

- [ ] **Step 2: Verify Tunnel Log Output**

Run: `docker compose logs tunnel`
Expected: Log contains `https://....trycloudflare.com`.

- [ ] **Step 3: Test Health Endpoint**

Run: `powershell -Command "(docker compose logs tunnel | Select-String 'trycloudflare.com' | Select-Object -Last 1).Matches[0].Value + '/healthz'"`
Verify `curl` or browser returns health status.

- [ ] **Step 4: Build APK**

Run: `make dev-apk-tunnel`
Expected: Build succeeds and produces `apps/mobile-kmm/androidApp/build/outputs/apk/debug/androidApp-debug.apk`.
