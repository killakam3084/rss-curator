# Login — obtains a session cookie for use by subsequent smoke tests.
#
# In TrueNAS/sidecar mode (run with --cookie-jar): valid credentials
# produce a 302 → "/" redirect and the curator_session cookie is saved
# to the jar for all subsequent files.
#
# In CI mode (no auth middleware): env credentials are "unused", so the
# login fails with 302 → "/login?error=1" and no cookie is set. That is
# fine — the curator_session cookie is never checked in CI mode because
# CURATOR_PASSWORD is unset and the auth middleware is not active.
#
# Accept any 3xx — success and failure redirects are both acceptable here.

POST {{base}}/login
[Options]
location: false
[FormParams]
username: {{username}}
password: {{password}}

HTTP *
[Asserts]
status >= 300
status < 400
