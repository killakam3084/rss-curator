# Scheduler — task list, on-demand run, and conflict detection.

# Task list — must return an array (registered once at startup with feed_check).
GET {{base}}/api/scheduler/tasks

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$"       isCollection
jsonpath "$[0].type" isString


# Trigger feed_check on-demand → 202 Accepted.
# Note: this starts a real feed check in the background; on TrueNAS it hits
# the live RSS feed. The scheduler deduplicates concurrent runs so calling
# it once here is safe.
POST {{base}}/api/scheduler/run/feed_check

HTTP 202
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.status" == "accepted"
jsonpath "$.type"   == "feed_check"


# Trigger watchlist_enrich on-demand → 202 Accepted.
POST {{base}}/api/scheduler/run/watchlist_enrich

HTTP 202
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.status" == "accepted"
jsonpath "$.type"   == "watchlist_enrich"


# Unknown task type — RunNow returns false → 409 Conflict.
POST {{base}}/api/scheduler/run/nonexistent_task_type

HTTP 409
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.error" isString
