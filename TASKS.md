# Work log

## COMPLETED — 2026-08-21 — zabbix-ai-cli v0.1 and cutover

Built the project, verified it against the live Zabbix 7.4.10, and replaced the
previous MCP server with it.

- Repository published at `github.com/stufently/zabbix-ai-cli`.
- Binary installed at `~/bin/zabbix-ai-cli`; profile `prod` carries the token
  transferred from the old container, with the `maintenance` and `acknowledge`
  scopes granted.
- Skills installed into `~/.claude/skills` and `~/.codex/skills`.
- The `zabbix` MCP entry in `~/.claude.json` now runs
  `/home/deploy/bin/zabbix-ai-cli mcp --profile prod` over stdio. The old
  container `zabbix-mcp-server` is stopped and removed; its image is kept.
- **Takes effect on the next Claude Code restart.**

Rollback, if it is ever needed: `docker compose up -d` in
`~/services/zabbix-mcp-server`, then restore `~/.claude.json` from the
`.bak-20260821-055539` copy alongside it.

What changed in the code is in CHANGELOG.md; the design and its reasoning are in
`docs/superpowers/specs/2026-08-21-zabbix-ai-cli-design.md`.

## IN_PROGRESS

Nothing.
