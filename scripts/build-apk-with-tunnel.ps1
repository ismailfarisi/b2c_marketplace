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
