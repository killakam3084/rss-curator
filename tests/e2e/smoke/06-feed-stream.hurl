# Feed stream — raw RSS feed discovery log.
# On a fresh CI DB there are no raw items; on TrueNAS there may be many.

GET {{base}}/api/feed/stream

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.items" isCollection
jsonpath "$.total" isInteger
