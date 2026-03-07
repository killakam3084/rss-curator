# Copilot Repository Instructions

## Primary Workflow Skill
For any task that results in a contribution intended for commit/release, follow:

- `.github/skills/contribution-workflow/SKILL.md`

## Additional Repo Guidance
- Prefer minimal, targeted diffs.
- Run formatting, vet, tests, and build checks before proposing commit/tag/push.
- Update `CHANGELOG.md` for release-relevant changes.
- Keep release tags semantic (`vMAJOR.MINOR.PATCH`).

## Terminal Output Rules
- **Never redirect or suppress stdout/stderr** from terminal commands (no `2>/dev/null`, no `| head`, no `&>/dev/null`, no `>/dev/null`) unless the command is genuinely noisy and the suppression is explicitly requested by the user.
- **Never truncate, pipe, or filter command output** (e.g. `| head -5`, `| tail -5`, `| grep …`) when running quality gates (`gofmt`, `go vet`, `go test`, `go build`) — full output must be visible so the user can verify results independently.
- **Do not append `&& echo "OK"` or similar synthetic success signals** as a substitute for showing real output — let the actual command output speak for itself.
- Exception: `grep`/`find` used purely for search/audit queries (not quality gates) may filter output for readability.