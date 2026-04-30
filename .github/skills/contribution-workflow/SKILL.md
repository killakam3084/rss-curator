# Contribution Workflow Skill

## Purpose
Use this skill for any code contribution that may be committed and released.

## Scope
Applies to changes in backend, frontend, infrastructure, and docs.

## Phased Delivery (default for multi-step features)
When a feature spans multiple logical layers (e.g., storage → API → UI), implement and commit each phase as a self-contained unit rather than one large diff.

**Phase definition rules:**
- Each phase must compile and pass all tests on its own.
- Phases should follow dependency order (lower layers first: storage → API → frontend → docs).
- Each phase gets its own commit with a clear, scoped message.
- Only the final phase of a release milestone updates `CHANGELOG.md` and receives the release tag.
- Intermediate phases are committed to `main` without a release tag.

**Example phase breakdown for a feature:**
```
Phase 1: internal/logbuffer — new package, no external deps
Phase 2: internal/storage   — new interface methods + implementation
Phase 3: internal/api       — wire new package + expose endpoints
Phase 4: cmd/               — update entrypoint wiring
Phase 5: web/               — frontend consuming new API
Phase 6: docs + CHANGELOG   — user-facing summary + release tag
```

## Required Flow
1. **Understand scope first**
   - Confirm requested behavior and impacted files.
   - Avoid unrelated refactors.
   - Identify phase breakdown before starting.

2. **Implement changes**
   - Keep diffs small and task-focused.
   - Preserve existing naming/style patterns.
   - One logical concern per commit — do not mix unrelated files.
   - **File creation/corruption recovery:** If a newly created or inserted file ends up with mangled content (reversed lines, duplicate declarations, encoding artifacts), do NOT attempt to fix it in-place with large string replacements or HEREDOC terminal commands — both approaches are brittle on large files. Instead: `rm` the file, then recreate it from scratch with the `create_file` tool. Verify with a targeted `read_file` after creation before running quality gates.

3. **Run local quality gates before each commit**
   - Format: `gofmt -w .`
     - **Do not manually align struct field names or tags with extra spaces.** `gofmt` does not produce aligned padding and the CI tollgate (`test -z "$(gofmt -l .)"`) will fail if hand-aligned whitespace is present. Always let `gofmt -w` be the sole authority on spacing.
   - Lint/static checks: `go vet ./...`
   - Unit tests: `go test ./... -v`
   - Build verification: `go build -o curator ./cmd/curator`

4. **Update changelog before final commit only**
   - Add a new version section at top of `CHANGELOG.md`.
   - Use Keep a Changelog sections (`Added`, `Changed`, `Fixed`, etc.).
   - Keep entries concise and user-facing.
   - Intermediate phase commits do NOT touch `CHANGELOG.md`.

5. **Commit with clear message**
   - Stage only the files belonging to that phase.
   - Use imperative, scoped commit subject: `feat(scope): description`
   - Include short bullet summary in commit body for multi-file changes.
   - Example subjects:
     - `feat(logbuffer): add ring buffer + zapcore integration`
     - `feat(storage): add GetWindowStats for 24h windowed counts`
     - `feat(api): expose /api/stats window fields and /api/logs SSE stream`
     - `feat(ui): stats mini-panel and log drawer`
     - `chore: CHANGELOG and version bump for v0.15.0`

6. **Tag release when requested**
   - Apply tag only on the final phase commit (changelog + version bump).
   - Use semantic version tags: `vMAJOR.MINOR.PATCH`.
   - Annotated tag format:
     - `git tag -a vX.Y.Z -m "Release vX.Y.Z: <summary>"`

7. **Push in order**
   - `git push`
   - `git push --tags` (when a tag was created)

8. **Post-push verification**
   - Confirm CI is running and green.
   - If failures occur, fix-forward with a focused follow-up commit.

## CI Expectations
- Build/push jobs must depend on passing test/lint phases.
- Do not bypass failing test/lint checks.

## Functional (E2E) Test Coverage

Every contribution that adds or changes an API endpoint, auth behaviour, or scheduler/job interaction must include corresponding Hurl smoke test coverage. This is not optional — the E2E suite is a publish gate on tagged releases.

### Where tests live

```
tests/e2e/smoke/    — shape-safe assertions; run against CI and live TrueNAS
tests/e2e/auth/     — auth flow tests; run separately (no shared cookie jar)
```

### Coverage expectations by change type

| Change type | Required coverage |
|---|---|
| New `GET` endpoint | New or updated `smoke/NN-<name>.hurl` asserting status 200 and response shape |
| New `POST` endpoint | Smoke test for validation error paths (400/405); mutation test if it mutates state |
| Auth behavour change | Update `auth/auth-flow.hurl` to reflect new rules |
| Scheduler/job change | Update `smoke/07-scheduler.hurl` or relevant job assertions |
| Bug fix | Add a regression assertion to the relevant smoke file |

### Hurl authoring rules

- Use `{{base}}`, `{{username}}`, `{{password}}` template variables — never hardcode hosts or credentials.
- Prefer `jsonpath "$.field" isString` / `isInteger` / `isCollection` shape assertions over exact-value assertions where the value varies by environment.
- Exact-value assertions (`== "healthy"`, `== "accepted"`) are fine when the value is a constant defined in the Go source.
- Use `HTTP *` + `status >= 400` when the exact status code legitimately differs between CI and live (e.g. 503 when AI is absent vs 400 when it is present).
- Keep each `.hurl` file limited to one logical concern; add a new file rather than appending unrelated requests to an existing one.
- Number files with a two-digit prefix (`NN-`) so execution order is deterministic.

### Running locally

```bash
# Full CI stack (fresh DB, no auth)
make test-e2e

# Against live TrueNAS stack
export CURATOR_USERNAME=admin CURATOR_PASSWORD=<secret>
make validate-smoke
```

### Phase placement

E2E test files are part of the same phase as the API change they cover. A new endpoint in Phase 3 (`internal/api`) gets its smoke test committed in Phase 3 — not deferred to a later phase. The final release phase (`chore: CHANGELOG and version bump`) should not be the first time E2E coverage appears.

## Safety Rules
- Never commit secrets or local env files.
- Never modify unrelated files to satisfy checks.
- If a check fails due to unrelated legacy issues, report clearly and isolate your change.