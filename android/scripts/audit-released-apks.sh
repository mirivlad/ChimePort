#!/usr/bin/env bash
set -euo pipefail

# Inspect the APKs published to GitHub Releases, not merely the keystore.
repo="${GITHUB_REPOSITORY:-mirivlad/ChimePort}"
sdk="${ANDROID_HOME:-/usr/local/lib/android/sdk}"
signer="$sdk/build-tools/35.0.0/apksigner"
aapt="$sdk/build-tools/35.0.0/aapt"
test -x "$signer"
test -x "$aapt"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for release in 5 6; do
  apk="$tmp/alpha-$release.apk"
  curl --fail --silent --show-error --location --retry 3 \
    "https://github.com/$repo/releases/download/android-alpha-$release/ChimePort-android-alpha.apk" \
    --output "$apk"
  "$signer" verify --verbose --print-certs "$apk" > "$tmp/alpha-$release.verify"
  cert="$(sed -n 's/^Signer #1 certificate SHA-256 digest: //p' "$tmp/alpha-$release.verify" | head -n 1)"
  pkg="$("$aapt" dump badging "$apk" | sed -n "s/^package: name='\([^']*\)'.*/\1/p" | head -n1)"
  version="$("$aapt" dump badging "$apk" | sed -n "s/^package:.*versionCode='\([^']*\)'.*/\1/p" | head -n1)"
  test -n "$cert"
  test -n "$pkg"
  echo "alpha-$release: package=$pkg versionCode=$version signer_sha256=$cert"
  printf '%s\n' "$cert" > "$tmp/alpha-$release.sha"
  printf '%s\n' "$pkg" > "$tmp/alpha-$release.pkg"
done

if ! cmp -s "$tmp/alpha-5.pkg" "$tmp/alpha-6.pkg"; then
  echo "::error::Published APK package names differ."
  exit 1
fi
if ! cmp -s "$tmp/alpha-5.sha" "$tmp/alpha-6.sha"; then
  echo "::warning::Historical alpha-5 and alpha-6 APK signers differ. The Android installation failure is expected."
  exit 0
fi
echo "Published alpha-5 and alpha-6 APKs are signed by the same certificate."
