# Health check — should return 200 with status "healthy" on every environment.
# This endpoint is auth-exempt (see authMiddleware exempt list).

GET {{base}}/api/health

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.status" == "healthy"
