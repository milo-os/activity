# milo-activity Claude Code plugin

Query audit logs, investigate incidents, and author ActivityPolicies with the
Activity service from Claude Code.

Install it from the Datum Cloud marketplace:

```bash
claude plugin marketplace add datum-cloud/claude-code-plugins
claude plugin install milo-activity@datum-claude-code-plugins
```

The `activity` binary must be on your PATH and configured as an MCP server
separately; see [docs/guides/mcp-server.md](../docs/guides/mcp-server.md).

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
