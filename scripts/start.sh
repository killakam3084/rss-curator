#!/bin/sh
# RSS Curator Start Script
# Orchestrates scheduler (background) + API server (foreground)
# This allows both periodic checks and HTTP-based review operations

set -e

LOG_FILE=${LOG_FILE:-/app/logs/curator.log}
mkdir -p "$(dirname "$LOG_FILE")"

echo "$(date '+%Y-%m-%d %H:%M:%S') - RSS Curator starting in dual-mode (scheduler + API)" | tee -a "$LOG_FILE"

# Trap SIGTERM and SIGINT for graceful shutdown
trap 'echo "$(date "+%Y-%m-%d %H:%M:%S") - Shutting down gracefully..." | tee -a "$LOG_FILE"; kill $(jobs -p) 2>/dev/null; wait; exit 0' SIGTERM SIGINT

# Start scheduler in background
echo "$(date '+%Y-%m-%d %H:%M:%S') - Starting scheduler (interval: ${CHECK_INTERVAL:-3600}s)..." | tee -a "$LOG_FILE"
/app/scheduler.sh >> "$LOG_FILE" 2>&1 &
SCHEDULER_PID=$!
echo "$(date '+%Y-%m-%d %H:%M:%S') - Scheduler PID: $SCHEDULER_PID" | tee -a "$LOG_FILE"

# Brief pause to let scheduler start
sleep 1

# Start API server in foreground
echo "$(date '+%Y-%m-%d %H:%M:%S') - Starting API server (port: ${CURATOR_API_PORT:-8081})..." | tee -a "$LOG_FILE"
exec /app/curator serve
