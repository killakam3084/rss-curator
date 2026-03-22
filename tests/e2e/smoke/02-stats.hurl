# Stats — 24-hour windowed torrent counts.
# On both CI (empty DB) and TrueNAS (live DB) all numeric fields are present.

GET {{base}}/api/stats

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.hours"    isInteger
jsonpath "$.pending"  isInteger
jsonpath "$.seen"     isInteger
jsonpath "$.staged"   isInteger
jsonpath "$.approved" isInteger
jsonpath "$.rejected" isInteger
jsonpath "$.queued"   isInteger
