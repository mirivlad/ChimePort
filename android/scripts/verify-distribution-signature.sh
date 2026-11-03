#!/usr/bin/env bash
set -euo pipefail

apk="${1:?Usage: verify-distribution-signature.sh path/to/app.apk}"
sdk="${ANDROID_HOME:-/usr/local/lib/android/sdk}"
signer="$sdk/build-tools/35.0.0/apksigner"
aapt="$sdk/build-tools/35.0.0/aapt"
expected="$(tr '[:upper:]' '[:lower:]' < android/signing/expected-alpha-sha256.txt | tr -d '[:space:]')"
test -x "$signer"
test -x "$aapt"
test -f "$apk"
if [ "${#expected}" -ne 64 ]; then
  echo "::error::Pinned SHA-256 fingerprint must contain exactly 64 hex characters."
  exit 1
fi

actual="$("$signer" verify --verbose --print-certs "$apk" | sed -n 's/^Signer #1 certificate SHA-256 digest: //p' | head -n1)"
pkg="$("$aapt" dump badging "$apk" | sed -n "s/^package: name='\([^']*\)'.*/\1/p" | head -n1)"
version="$("$aapt" dump badging "$apk" | sed -n "s/^package:.*versionCode='\([^']*\)'.*/\1/p" | head -n1)"
echo "Distributed APK: package=$pkg versionCode=$version signer_sha256=$actual"
echo "Pinned release signer: $expected"
if [ "$pkg" != "top.mirv.chimeport" ]; then
  echo "::error::Package ID changed, aborting release."
  exit 1
fi
if [ "$actual" != "$expected" ]; then
  echo "::error::APK is not signed with the pinned alpha identity. Refusing to publish."
  exit 1
fi
echo "Signing certificate matches pinned alpha identity."
