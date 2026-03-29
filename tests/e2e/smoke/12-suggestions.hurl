# Suggestions — GET /api/suggestions/status probe + POST /api/suggestions.

# GET status — always returns 200 with available bool and shows_count int.
GET {{base}}/api/suggestions/status

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.available"    isBoolean
jsonpath "$.shows_count"  isInteger


# POST — when AI is unavailable the service returns 503.
# When AI is available and shows are configured it returns 200 with suggestions.
# We can only assert the response shape here since CI has no live LLM.
POST {{base}}/api/suggestions
Content-Type: application/json
```json
{"limit": 3}
```

[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.status"       isString
jsonpath "$.suggestions"  isCollection


# POST — wrong method guard (GET not allowed).
GET {{base}}/api/suggestions

HTTP 405
