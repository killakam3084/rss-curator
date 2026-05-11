# Auth flow verification — TrueNAS/sidecar mode only.
#
# Run this file in a SEPARATE hurl invocation WITHOUT --cookie-jar so the
# session state from the smoke tests does not bleed in:
#
#   hurl --test \
#     --variable base=http://localhost:8081 \
#     tests/e2e/auth/auth-flow.hurl
#
# (docker-compose.validate.yml handles this automatically.)


# ── 1. Unauthenticated API request → 401 JSON ─────────────────────────────
GET {{base}}/api/torrents

HTTP 401
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.error" == "unauthorized"


# ── 2. Bad credentials → redirect to /login?error=1 ──────────────────────
POST {{base}}/login
[Options]
location: false
[FormParams]
username: wrong_user
password: wrong_pass

HTTP 302
[Asserts]
header "Location" contains "error=1"


# ── 3. Correct credentials → 302 to "/" + Set-Cookie ─────────────────────
POST {{base}}/login
[Options]
location: false
[FormParams]
username: {{username}}
password: {{password}}

HTTP 302
[Asserts]
header "Location" == "/"
[Captures]
session_cookie: cookie "curator_session"


# ── 4. Authenticated request with captured cookie → 200 ───────────────────
GET {{base}}/api/torrents
Cookie: curator_session={{session_cookie}}

HTTP 200
[Asserts]
header "Content-Type" contains "application/json"
jsonpath "$.torrents" isCollection
jsonpath "$.count"    isInteger


# ── 5. /login/ (trailing slash) → 301 to /login ───────────────────────────
# Regression: /login/?next=/ previously returned 404 after a successful login
# because ServeMux had no handler for /login/ and the auth middleware did not
# exempt it.
GET {{base}}/login/
[Options]
location: false

HTTP 301
[Asserts]
header "Location" == "/login"


# ── 6. /login/?next=/ → 301 to /login?next=/ (query preserved) ───────────
GET {{base}}/login/?next=/
[Options]
location: false

HTTP 301
[Asserts]
header "Location" == "/login?next=/"
