# Jobs — list and single-record lookup.
# On a fresh CI DB the list must be an array (possibly empty).
# A request for a non-existent job ID must return 404.

GET {{base}}/api/jobs

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$" isCollection


# Non-existent job → 404
GET {{base}}/api/jobs/99999

HTTP 404


# Cancel requires numeric ID
POST {{base}}/api/jobs/nope/cancel

HTTP 400


# Cancelling a non-existent job should return 404
POST {{base}}/api/jobs/99999/cancel

HTTP 404
