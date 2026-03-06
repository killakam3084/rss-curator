# Contribution Workflow Skill

## Purpose
Use this skill for any code contribution that may be committed and released.

## Scope
Applies to changes in backend, frontend, infrastructure, and docs.

## Required Flow
1. **Understand scope first**
   - Confirm requested behavior and impacted files.
   - Avoid unrelated refactors.

2. **Implement changes**
   - Keep diffs small and task-focused.
   - Preserve existing naming/style patterns.

3. **Run local quality gates**
   - Format: `gofmt -w .`
   - Lint/static checks: `go vet ./...`
   - Unit tests: `go test ./... -v`
   - Build verification: `go build -o curator ./cmd/curator`

4. **Update changelog before commit**
   - Add a new version section at top of `CHANGELOG.md`.
   - Use Keep a Changelog sections (`Added`, `Changed`, `Fixed`, etc.).
   - Keep entries concise and user-facing.

5. **Commit with clear message**
   - Stage only relevant files.
   - Use imperative, specific commit subject.
   - Include short bullet summary in commit body for multi-file changes.

6. **Tag release when requested**
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

## Safety Rules
- Never commit secrets or local env files.
- Never modify unrelated files to satisfy checks.
- If a check fails due to unrelated legacy issues, report clearly and isolate your change.