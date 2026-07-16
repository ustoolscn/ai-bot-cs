#!/bin/sh
set -eu

data_dir="${DATA_DIR:-/app/data}"
mkdir -p "$data_dir"
chown -R aibot:aibot "$data_dir"

exec su-exec aibot:aibot /app/ai-bot "$@"
