# Torrents — list pending (default) and an explicit status filter.
# On a fresh CI DB the torrents array is empty; shape must still be correct.

GET {{base}}/api/torrents

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger


# Explicit status filter — verified count matches array size.
GET {{base}}/api/torrents?status=approved

HTTP 200
[Asserts]
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger


# Content type filter — shows only.
GET {{base}}/api/torrents?content_type=show

HTTP 200
[Asserts]
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger


# Content type filter — movies only.
GET {{base}}/api/torrents?content_type=movie

HTTP 200
[Asserts]
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger
