#!/bin/sh
# RSS Curator Scheduler
# Runs periodic checks on a defined interval

# Configuration
CHECK_INTERVAL=${CHECK_INTERVAL:-3600}  # Default: 1 hour (in seconds)
LOG_FILE=${LOG_FILE:-/app/logs/curator.log}

# Ensure log directory exists
mkdir -p "$(dirname "$LOG_FILE")"

echo "$(date '+%Y-%m-%d %H:%M:%S') - RSS Curator Scheduler started (interval: ${CHECK_INTERVAL}s)" | tee -a "$LOG_FILE"

# Trap SIGTERM to gracefully shutdown
trap 'echo "$(date "+%Y-%m-%d %H:%M:%S") - Scheduler shutting down..." | tee -a "$LOG_FILE"; exit 0' SIGTERM SIGINT

# Main loop
while true; do
    echo "$(date '+%Y-%m-%d %H:%M:%S') - Starting scheduled check..." | tee -a "$LOG_FILE"
    
    # Run the check and append output to log
    /app/curator check >> "$LOG_FILE" 2>&1
    CHECK_EXIT=$?
    
    if [ $CHECK_EXIT -eq 0 ]; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') - Check completed successfully" | tee -a "$LOG_FILE"
    else
        echo "$(date '+%Y-%m-%d %H:%M:%S') - Check failed with exit code: $CHECK_EXIT" | tee -a "$LOG_FILE"
    fi
    
    # Wait for next interval
    echo "$(date '+%Y-%m-%d %H:%M:%S') - Next check in ${CHECK_INTERVAL}s..." | tee -a "$LOG_FILE"
    sleep "$CHECK_INTERVAL"
done
