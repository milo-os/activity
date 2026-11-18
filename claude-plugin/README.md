# milo-activity Claude Code plugin

Query audit logs, investigate incidents, and author and test ActivityPolicies
with the Activity service from Claude Code.

Install it from the Datum Cloud marketplace:

```bash
claude plugin marketplace add datum-cloud/claude-code-plugins
claude plugin install milo-activity@datum-claude-code-plugins
```

The `activity` binary must be on your PATH and configured as an MCP server
separately for live cluster tools; see [docs/guides/mcp-server.md](../docs/guides/mcp-server.md).
Offline policy validation and unit testing use `activitytool` (see
[docs/guides/testing-policies.md](../docs/guides/testing-policies.md)), which
other repositories run as the `ghcr.io/milo-os/activitytool` container image.

## Skills

| Skill | Purpose |
|---|---|
| `author-policy` | Create or edit an `ActivityPolicy` from live audit/event samples |
| `preview-policy` | Preview an `ActivityPolicy` against live cluster samples |
| `test-policy` | Validate and unit-test `ActivityPolicy` rules offline with `activitytool`, and wire CI |
| `investigate` | Start an incident investigation |
| `investigate-incident` | Investigate cluster incidents via audit logs and events |
| `audit-user` | Audit a user's recent activity |
| `activity-summary` | Summarize recent cluster activity |
| `status` | Quick recent cluster activity summary |

## Developing the plugin

Load your checkout directly instead of the published copy:

```bash
claude --plugin-dir ./claude-plugin
```

Validate before opening a pull request:

```bash
claude plugin validate ./claude-plugin
```

Bump `version` in `.claude-plugin/plugin.json` on every change that should
reach users; the marketplace only ships a new copy when it changes.
