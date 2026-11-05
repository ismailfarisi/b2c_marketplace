#!/usr/bin/env bash
#
# Fails when the iOS Release configuration could ship pointing at a machine a
# phone cannot reach. Checks Release.xcconfig and every file it #includes that
# is in the repo, ignoring comments, and requires both base URLs to be https.
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../iosApp" && pwd)"
release="$dir/Release.xcconfig"
[ -f "$release" ] || { echo "::error::$release is missing"; exit 1; }

files=("$release")
while read -r inc; do
  [ -f "$dir/$inc" ] && files+=("$dir/$inc")
done < <(sed -nE 's/^[[:space:]]*#include\??[[:space:]]+"([^"]+)".*/\1/p' "$release")

status=0
for f in "${files[@]}"; do
  if sed -E 's|^[[:space:]]*//.*$||' "$f" | grep -nEi 'localhost|127\.0\.0\.1|0\.0\.0\.0|10\.0\.2\.2'; then
    echo "::error file=$f::Release config names a local host"
    status=1
  fi
done

for key in API_BASE_URL WEB_BASE_URL; do
  line=$(cat "${files[@]}" | grep -E "^[[:space:]]*$key[[:space:]]*=" | tail -1 || true)
  if [ -z "$line" ]; then
    echo "::error file=$release::$key is not set for Release"
    status=1
  elif ! echo "$line" | grep -qE '=[[:space:]]*https:'; then
    echo "::error file=$release::$key must be https in Release: $line"
    status=1
  fi
done

[ "$status" = 0 ] && echo "Release xcconfig OK: ${files[*]##*/}"
exit "$status"
