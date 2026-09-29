#!/bin/sh
set -eu

prefix="${RESULTS_PREFIX:-uat-mesh}"
jar=/tmp/hurl_cookies.jar
rm -f "$jar"

for i in $(seq 1 30); do
  wget -q --spider "$HURL_base/api/health" 2>/dev/null && break
  sleep 1
done

for f in \
  /tests/e2e/smoke/00-login.hurl \
  /tests/e2e/smoke/01-health.hurl \
  /tests/e2e/smoke/02-stats.hurl \
  /tests/e2e/smoke/03-activity.hurl \
  /tests/e2e/smoke/04-jobs.hurl \
  /tests/e2e/smoke/05-torrents.hurl \
  /tests/e2e/smoke/06-feed-stream.hurl \
  /tests/e2e/smoke/07-scheduler.hurl \
  /tests/e2e/smoke/10-alerts.hurl \
  /tests/e2e/smoke/13-plex.hurl \
  /tests/e2e/smoke/14-plex-functional.hurl; do
  name=$(basename "$f" .hurl)
  if [ "$f" = "/tests/e2e/smoke/00-login.hurl" ]; then
    hurl --test -c "$jar" --report-junit "/results/${prefix}-${name}.xml" "$f"
  else
    hurl --test -b "$jar" -c "$jar" --report-junit "/results/${prefix}-${name}.xml" "$f"
  fi
done

hurl --test --report-junit "/results/${prefix}-auth.xml" /tests/e2e/auth/auth-flow.hurl
