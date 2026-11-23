#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL must be set for container startup}"

data_dir="${DATA_DIR:-/app/data}"
mkdir -p "$data_dir"
chown -R aibot:aibot "$data_dir"

exec su-exec aibot:aibot /app/ai-bot "$@"
