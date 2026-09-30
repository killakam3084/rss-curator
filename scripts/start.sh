#!/bin/sh
# RSS Curator Start Script
# The curator server owns the scheduler and API in one process.

set -e

LOG_FILE=${LOG_FILE:-/app/logs/curator.log}
mkdir -p "$(dirname "$LOG_FILE")"

echo "$(date '+%Y-%m-%d %H:%M:%S') - RSS Curator starting in dual-mode (scheduler + API)" | tee -a "$LOG_FILE"

# Start the API server and its built-in scheduler in the foreground.
echo "$(date '+%Y-%m-%d %H:%M:%S') - Starting curator server (API + scheduler, port: ${CURATOR_API_PORT:-8081})..." | tee -a "$LOG_FILE"
exec /app/curator serve
