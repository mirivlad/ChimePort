#!/bin/sh
# Online ChimePort backup through a temporary Docker container.
# Must run on the Docker host with Docker access.
set -eu

DATA_SOURCE="${CHIMEPORT_DATA_SOURCE:-chimeport_chimeport_data}"
BACKUP_DIR="${CHIMEPORT_BACKUP_DIR:-/srv/chimeport/backups}"
IMAGE="${CHIMEPORT_IMAGE:-ghcr.io/mirivlad/chimeport:main}"

if [ ! -d "$BACKUP_DIR" ]; then
  echo "Backup directory does not exist: $BACKUP_DIR" >&2
  echo "Create it first with write access for UID 10001." >&2
  exit 1
fi

case "$DATA_SOURCE" in
  /*)
    if [ ! -d "$DATA_SOURCE" ]; then
      echo "Live data bind path does not exist: $DATA_SOURCE" >&2
      exit 1
    fi
    data_mount="type=bind,source=$DATA_SOURCE,target=/data"
    ;;
  *)
    if ! docker volume inspect "$DATA_SOURCE" >/dev/null 2>&1; then
      echo "Missing existing Docker volume: $DATA_SOURCE" >&2
      exit 1
    fi
    data_mount="type=volume,source=$DATA_SOURCE,target=/data"
    ;;
esac

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive="chimeport-$stamp.tar.gz"

docker run --rm --network none --user 10001:10001 \
  --security-opt no-new-privileges:true --cap-drop ALL \
  --mount "$data_mount" \
  --mount "type=bind,source=$BACKUP_DIR,target=/backups" \
  --env "CHIMEPORT_IMAGE_REF=$IMAGE" \
  "$IMAGE" maintenance backup --data-dir /data --output "/backups/$archive"

echo "Verified backup: $BACKUP_DIR/$archive"
echo "Keep an encrypted copy on a different host or disk."
