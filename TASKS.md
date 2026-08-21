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

## COMPLETED — 2026-08-21 — review round two

Codex and an independent code review of the finished implementation; every
finding checked against the code before acting, since both reviewers produced
some that did not hold. What was real is fixed and listed under Security and
Fixed in CHANGELOG.md. The headline ones: `--http :8000` bound every interface
while counting as loopback, `api call` allowed `script.create` and
`action.create` (a longer road to the command execution `script.execute` is
refused for), a plan's risk and scope were trusted from the file rather than
re-derived, and the container could not start because its state directory did
not exist.

CI also failed on `golangci-lint`: the action resolved to a v1 build made with
an older Go than the module targets. Pinned to v2.12.2 and the `go` directive
lowered to 1.25.0, which is what the MCP SDK actually needs and what the README
promises.

## COMPLETED — 2026-08-21 — review of the Go 1.27 pass

Eight findings, six real. Fixed with tests: `plans list` aborting when one plan
was claimed under it, `zabbix_plan_status` stuck on "applying" behind a leftover
claim, `alert why` making two serial `user.get` calls per candidate, unsanitised
`read_error` reaching the terminal, numeric host names failing to resolve, and
`--store` validated only after `--token-stdin` had eaten the token. Also moved
the build cache out of the repository — it broke `make fmt-check` — and put the
`go` directive back to its real lower bound. Not acted on: the nil-`http.Client`
repair in `api.NewClient`, whose scenario `httpClient` cannot produce.

MCP binary on this host updated to `51f1643` via `make install`.

## COMPLETED — 2026-08-21 — public release v0.1.0 and discovery

The repository is public (gitleaks found nothing in its history first), tagged
v0.1.0, and published: release archives for six platforms, `ghcr.io/stufently/
zabbix-ai-cli` (anonymously pullable), and an entry in the official MCP registry
as `io.github.stufently/zabbix-ai-cli`.

Discovery work: repository description and 20 topics — `mcp-server` and
`model-context-protocol` are what Glama and PulseMCP crawl for; README rewritten
with per-client install snippets and a FAQ; CONTRIBUTING and a code of conduct.

The local MCP now runs the ghcr image instead of `~/bin`, pinned to `0.1.0`.
The profile is mounted read-only and the state directory read-write and shared,
so a plan created by an agent is still approved from the host with
`zabbix-ai-cli approve <id>`.

Rollback: restore `~/.claude.json` from `.bak-20260821-102243`, which points the
MCP back at `/home/deploy/bin/zabbix-ai-cli`.

Bumping the pinned image later means editing that one `args` entry in
`~/.claude.json` after the new tag's release finishes.

## IN_PROGRESS

Nothing.
