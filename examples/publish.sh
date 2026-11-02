#!/bin/sh
# Usage: CHIMEPORT_URL=https://notify.example.org CHIMEPORT_TOKEN=... ./publish.sh
set -eu
: "${CHIMEPORT_URL:?}" "${CHIMEPORT_TOKEN:?}"
curl --fail-with-body --show-error --silent \
  -X POST "${CHIMEPORT_URL%/}/api/v1/messages" \
  -H "Authorization: Bearer $CHIMEPORT_TOKEN" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(date +%s)-$$" \
  -d '{"channel":"home/security","title":"Door","body":"Front door opened","priority":"high"}'
