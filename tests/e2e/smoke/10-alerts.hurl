# Alerts — recent alert ring buffer.
# Returns a JSON array (may be empty on a fresh instance).

GET {{base}}/api/alerts

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$" isCollection
