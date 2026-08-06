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
