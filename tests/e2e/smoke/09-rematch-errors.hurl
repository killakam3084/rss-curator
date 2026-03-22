# Rematch validation errors.
# The matcher is always configured (SHOW_NAMES or shows.json), so the
# empty-IDs validation path fires in both CI and TrueNAS.

# Wrong HTTP method → 405.
GET {{base}}/api/torrents/rematch

HTTP 405


# POST with empty IDs → 400.
POST {{base}}/api/torrents/rematch
Content-Type: application/json
{"ids": []}

HTTP 400
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.error" isString


# POST with no body → 400 (decode fails, treated as empty IDs).
POST {{base}}/api/torrents/rematch
Content-Type: application/json
{}

HTTP 400
[Asserts]
jsonpath "$.error" isString
