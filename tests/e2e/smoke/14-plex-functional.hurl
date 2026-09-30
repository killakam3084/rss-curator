# Functional Plex mesh flow.
# The mesh serves deterministic RSS, Plex, qBittorrent and Ollama-compatible
# endpoints. Feed check stages the fixtures; Plex sync then annotates them.

# Stage the deterministic RSS candidates.
POST {{base}}/api/feed-check

HTTP 202
[Asserts]
jsonpath "$.job_id" isInteger
[Captures]
feed_job_id: jsonpath "$.job_id"

# Wait for the fixture torrents to be staged. This avoids relying on the
# global jobs list while the asynchronous feed check is still running.
GET {{base}}/api/torrents?status=pending
[Options]
retry: 20

HTTP 200
[Asserts]
jsonpath "$.count" == 3

# Sync the mock Plex library and reconcile staged candidates.
POST {{base}}/api/plex/sync

HTTP 202
[Asserts]
jsonpath "$.status" == "queued"

# The sync job is deduplicated as plex_sync; poll the job list until the most
# recent job completes. The dedicated status endpoint confirms cache contents.
GET {{base}}/api/plex/status
[Options]
retry: 20

HTTP 200
[Asserts]
jsonpath "$.counts.shows" isInteger
jsonpath "$.counts.episodes" isInteger
jsonpath "$.counts.movies" isInteger
jsonpath "$.annotations.in_library" isInteger
jsonpath "$.annotations.library_upgrade" isInteger

# The lower-quality MobLand fixture is already in Plex.
GET {{base}}/api/torrents?annotation=in_library
[Options]
retry: 20

HTTP 200
[Asserts]
jsonpath "$.count" == 1
jsonpath "$.torrents[0].annotations[0].kind" == "in_library"
jsonpath "$.torrents[0].annotations[0].source" == "plex"
jsonpath "$.torrents[0].annotations[0].detail.match_tier" isString
jsonpath "$.torrents[0].annotations[0].detail.plex_quality" == "2160P"

[Captures]
already_have_id: jsonpath "$.torrents[0].id"

# The Amadeus fixture is a quality upgrade over the 1080p Plex copy.
GET {{base}}/api/torrents?annotation=library_upgrade
[Options]
retry: 20

HTTP 200
[Asserts]
jsonpath "$.count" == 1
jsonpath "$.torrents[0].annotations[0].kind" == "library_upgrade"
jsonpath "$.torrents[0].annotations[0].detail.plex_quality" == "1080P"

[Captures]
upgrade_id: jsonpath "$.torrents[0].id"

# The manual approval and queue path reaches the qBittorrent mesh mock.
POST {{base}}/api/torrents/{{upgrade_id}}/approve

HTTP 200
[Asserts]
jsonpath "$.status" == "accepted"

POST {{base}}/api/torrents/{{upgrade_id}}/queue
Content-Type: application/json
{}

HTTP 200
[Asserts]
jsonpath "$.status" == "queued"

GET http://mesh:32400/health

HTTP 200
[Asserts]
jsonpath "$.added[0]" == "magnet:?xt=urn:btih:uat-amadeus"

# Manual cleanup uses the existing already_have action; Plex never changes
# the status automatically.
POST {{base}}/api/torrents/{{already_have_id}}/already-have

HTTP 200
[Asserts]
jsonpath "$.status" == "rejected"

GET {{base}}/api/torrents?annotation=in_library

HTTP 200
[Asserts]
jsonpath "$.count" == 0

# The mesh qBittorrent endpoint remains reachable for later approve/queue
# assertions; this scenario deliberately does not auto-act on the upgrade.
GET {{base}}/api/plex/meta

HTTP 200
[Asserts]
jsonpath "$.connected" == true
jsonpath "$.libraries" isCollection
