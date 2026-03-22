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
