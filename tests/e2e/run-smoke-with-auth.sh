#!/bin/sh
# Runs each smoke .hurl file as its own hurl invocation and threads the login
# cookie through via -b/-c: hurl's --cookie-jar only supports a single
# session, so it can't take all 11 smoke files in one invocation.
set -e

# podman-compose recreates dependent containers on every `run`, so curator's
# healthcheck passing doesn't guarantee its network is fully up yet — wait
# for a real response before starting the suite.
for i in $(seq 1 30); do
  wget -q --spider "$HURL_base/api/health" 2>/dev/null && break
  sleep 1
done

prefix="${RESULTS_PREFIX:-smoke}"
jar=/tmp/hurl_cookies.jar
rm -f "$jar"
first=1

for f in \
  /tests/e2e/smoke/00-login.hurl \
  /tests/e2e/smoke/01-health.hurl \
  /tests/e2e/smoke/02-stats.hurl \
  /tests/e2e/smoke/03-activity.hurl \
  /tests/e2e/smoke/04-jobs.hurl \
  /tests/e2e/smoke/05-torrents.hurl \
  /tests/e2e/smoke/06-feed-stream.hurl \
  /tests/e2e/smoke/07-scheduler.hurl \
  /tests/e2e/smoke/08-rescore-errors.hurl \
  /tests/e2e/smoke/09-rematch-errors.hurl \
  /tests/e2e/smoke/10-alerts.hurl; do
  name=$(basename "$f" .hurl)
  if [ "$first" = 1 ]; then
    hurl --test -c "$jar" --report-junit "/results/${prefix}-${name}.xml" "$f"
    first=0
  else
    hurl --test -b "$jar" -c "$jar" --report-junit "/results/${prefix}-${name}.xml" "$f"
  fi
done

hurl --test --report-junit "/results/${prefix}-auth.xml" /tests/e2e/auth/auth-flow.hurl
