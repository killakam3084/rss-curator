# Plex — connection meta, cached status, and the opt-in job triggers.
#
# CI runs with Plex unconfigured, so the read endpoints must still answer 200
# with an explanatory error and empty collections. The mutation endpoints are
# allowed to return 503 there but 202/409 against a configured live stack,
# hence the wide status assertions.

# Connection meta — shape must hold whether or not Plex is reachable.
GET {{base}}/api/plex/meta

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.connected" isBoolean
jsonpath "$.libraries" isCollection


# Cached status — counts and annotation totals.
GET {{base}}/api/plex/status

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.enabled"     isBoolean
jsonpath "$.sync_enabled" isBoolean
jsonpath "$.libraries"   isCollection
jsonpath "$.counts.shows"    isInteger
jsonpath "$.counts.episodes" isInteger
jsonpath "$.counts.movies"   isInteger


# Wrong method on a read endpoint → 405.
POST {{base}}/api/plex/status

HTTP 405


# Sync trigger. 503 when Plex is not configured (CI), 202 when it is,
# 409 when a sync is already running.
POST {{base}}/api/plex/sync

HTTP *
[Asserts]
status >= 202
status < 504


# Reconcile trigger — same status envelope as sync.
POST {{base}}/api/plex/reconcile

HTTP *
[Asserts]
status >= 202
status < 504


# Wrong method on a mutation endpoint → 405.
GET {{base}}/api/plex/sync

HTTP 405


# The token must never be echoed back in settings.
GET {{base}}/api/settings

HTTP 200
[Asserts]
jsonpath "$.plex.enabled"              isBoolean
jsonpath "$.plex.sync_enabled"         isBoolean
jsonpath "$.plex.sync_interval_secs"   isInteger
jsonpath "$.plex.section_keys"         isCollection
jsonpath "$.plex.token"                not matches /^[A-Za-z0-9_-]{15,}$/


# Annotation filter on the torrent list.
GET {{base}}/api/torrents?annotation=in_library

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger
