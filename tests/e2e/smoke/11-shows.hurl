# Shows — GET returns current config (or empty template), PUT saves and hot-reloads.

# GET — shape check; works whether or not a shows.json exists on disk.
GET {{base}}/api/shows

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.shows"        isCollection
jsonpath "$.movies"       isCollection
jsonpath "$.shows_count"  isInteger
jsonpath "$.movies_count" isInteger


# PUT — a config with both shows and movies; verify round-trip shape.
PUT {{base}}/api/shows
Content-Type: application/json
```json
{
  "shows": [
    { "name": "Test Show", "min_quality": "1080p" }
  ],
  "movies": [
    { "name": "Test Movie", "min_quality": "1080p" }
  ],
  "defaults": {
    "min_quality": "720p",
    "preferred_codec": "",
    "preferred_groups": [],
    "exclude_groups": []
  }
}
```

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.shows"          isCollection
jsonpath "$.movies"         isCollection
jsonpath "$.shows_count"    == 1
jsonpath "$.movies_count"   == 1
jsonpath "$.shows[0].name"  == "Test Show"
jsonpath "$.movies[0].name" == "Test Movie"


# PUT — invalid JSON → 400 Bad Request.
PUT {{base}}/api/shows
Content-Type: application/json
```
{ not valid json
```

HTTP 400
[Asserts]
jsonpath "$.error" isString


# PUT — wrong method guard (PATCH not allowed).
PATCH {{base}}/api/shows
Content-Type: application/json
```json
{}
```

HTTP 405
[Asserts]
jsonpath "$.error" isString
