# Rescore validation errors.

# Wrong HTTP method → 405.
GET {{base}}/api/torrents/rescore

HTTP 405


# POST with empty IDs array.
# Behaviour differs by environment:
#   CI (no AI provider)      → 503 Service Unavailable (AI check fires first)
#   TrueNAS (AI available)   → 400 Bad Request (validation fires after AI check)
# Both are error responses — assert any 4xx/5xx rather than an exact code.
POST {{base}}/api/torrents/rescore
Content-Type: application/json
{"ids": []}

HTTP *
[Asserts]
status >= 400
jsonpath "$.error" isString
