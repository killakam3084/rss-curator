# Suggestions — GET /api/suggestions/status probe + GET /api/suggestions.

# GET status — always returns 200 with available bool, shows_count, and active_count.
GET {{base}}/api/suggestions/status

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.available"     isBoolean
jsonpath "$.shows_count"   isInteger
jsonpath "$.movies_count"  isInteger
jsonpath "$.active_count"  isInteger


# GET — returns 200 with a (possibly empty) suggestions array.
# Each element has the persistent row shape written by store.UpsertSuggestions.
# We can only assert the response shape here since CI has no live LLM.
GET {{base}}/api/suggestions

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.status"       isString
jsonpath "$.suggestions"  isCollection


# POST — wrong method guard (only GET is allowed).
POST {{base}}/api/suggestions
Content-Type: application/json
{}

HTTP 405
