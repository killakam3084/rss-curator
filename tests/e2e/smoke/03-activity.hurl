# Activity — paginated approve/reject log.
# On a fresh CI DB the list is empty; on TrueNAS it may have records.
# We assert only that the shape is correct.

GET {{base}}/api/activity

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.activities" isCollection
jsonpath "$.total"       isInteger
