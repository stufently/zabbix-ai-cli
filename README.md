# Zabbix AI CLI

AI-first CLI, MCP server and skills for Zabbix.
Built for Claude Code, Codex and AI agents.

`zabbix-ai-cli` is a single Go binary that turns Zabbix into a small set of
task-shaped commands an agent can be trusted to run: bounded output, a stable
JSON contract, and no change without a person's approval.

It never contacts a language model. The AI decides, this program executes,
Zabbix monitors.

```bash
zabbix-ai-cli login
zabbix-ai-cli problems list
zabbix-ai-cli host investigate server01
zabbix-ai-cli alert why 757474
zabbix-ai-cli mcp
```

## Why not another Zabbix API wrapper

Wrapping `host.get`, `problem.get` and `history.get` hands the agent the API's
sharp edges along with its power. On a current Zabbix 7.4 server:

- `problem.get` has no `selectHosts`, so a problem list arrives without hosts.
- `history.get` defaults to the numeric-unsigned table and returns **nothing** for
  a float item — silently, with no error.
- `item.lastvalue` and `item.lastclock` still exist and have returned a constant
  `"0"` for several major releases.
- `host.available` was removed in 5.4; availability lives on the interface.
- `event.acknowledge` takes a bitmask whose own documentation contradicts itself.
- `searchWildcardsEnabled: true` **disables** implicit substring matching, turning
  a name fragment into an exact match that quietly finds nothing.

Every one of those produces a confident, wrong answer rather than an error. This
tool absorbs them behind commands that describe the task instead of the endpoint.

## What it does

| Command | Answers |
| --- | --- |
| `problems list` | What is broken now — including suppressed problems, with the maintenance window that hides them named |
| `host investigate` | One call: host state, active problems, recent events, silent and unsupported items, maintenance |
| `host status` | A handful of fields instead of twelve thousand characters of configuration |
| `alert why` | Why a notification did or did not arrive — suppression, delivery attempts, actions, media types, per-recipient severity filters |
| `resolve` | Turns a notification pasted out of chat into event, host and trigger identifiers |
| `unreachable` | Monitored hosts Zabbix cannot poll, with the error it recorded |
| `metrics latest` / `history` | Values with the right history type, human units and `min/avg/max` |
| `maintenance` | Open, extend, end or remove windows, with host patterns like `ms*` |
| `api call` | The escape hatch, under the same approval rules |

## Safety

Read operations run immediately. Nothing else does.

| Caller | Read | Write | Destructive |
| --- | --- | --- | --- |
| CLI, a person at a terminal | immediate | plan, then `--apply` | plan, then `--apply --confirm <exact name>` |
| MCP, an agent | immediate | plan, then `approve` at a terminal | plan, then `approve` at a terminal |

There is no MCP parameter that applies a change, and adding one would not help:
a confirmation an agent can send is a confirmation prompt injection can send.
The approval lives at a terminal, outside the model's context.

```
$ zabbix-ai-cli maintenance create "ms*" --for 2h

PLAN pl_cc89d2e87d15

Create maintenance "ms* (2h0m)" for 2h0m, 2026-08-21T05:39:00Z to 2026-08-21T07:39:00Z

Affects:
  host ms1.8qw.ru
  host ms10.8qw.ru
  ...

Risk: write
Expires: 2026-08-21T05:54:22Z

Nothing has changed yet.
To apply it: zabbix-ai-cli approve pl_cc89d2e87d15
```

Before a plan runs, its parameters are re-hashed, its deadline checked and its
preconditions re-read from Zabbix. A window that has been replaced since the plan
was made is refused, not deleted. Every applied change is appended to an audit log.

This matters more than a refusal would. When the tool this replaces blocked a
write, the work was done anyway with a token copied out of a container — losing
the audit trail without preventing anything. A permitted path that is recorded
beats a refusal that gets routed around.

## Install

```bash
go install github.com/stufently/zabbix-ai-cli/cmd/zabbix-ai-cli@latest
```

Or build from source (Go 1.25 or newer):

```bash
make build      # native binary in ./bin
make docker     # container image for the MCP server
```

## Configure

```bash
zabbix-ai-cli login --profile prod
```

It asks for the URL and the API token, verifies the token against the server, and
stores it. The token is never accepted as a flag, because flag values are visible
in shell history and in the process list; pipe it in instead:

```bash
printf %s "$TOKEN" | zabbix-ai-cli login --profile prod --url https://zabbix.example.com --token-stdin
```

Writes are off until a profile is granted the scope for them:

```bash
zabbix-ai-cli profile scopes prod --add maintenance
```

See [docs/authentication.md](docs/authentication.md) for the resolution order and
the headless and container cases.

## Claude Code

```bash
claude mcp add zabbix -- zabbix-ai-cli mcp --profile prod
zabbix-ai-cli skills install claude
```

The MCP client never sees the Zabbix token: it is resolved inside the server
process from the profile you configured.

## Codex

```toml
# ~/.codex/config.toml
[mcp_servers.zabbix]
command = "zabbix-ai-cli"
args = ["mcp", "--profile", "prod"]
```

```bash
zabbix-ai-cli skills install codex
```

## MCP tools

Thirteen tools, not two hundred. A large tool surface costs an agent context
before it has done anything, and most of it is never called.

```
zabbix_problems           zabbix_metrics_latest      zabbix_unreachable
zabbix_problem            zabbix_metrics_history     zabbix_maintenance_list
zabbix_hosts              zabbix_alert_why           zabbix_api_call
zabbix_host_status        zabbix_resolve             zabbix_plan_create
zabbix_host_investigate                              zabbix_plan_status
```

Write operations do not get one tool each. `zabbix_plan_create` takes an
`operation` enum generated from the same registry the CLI is built from, so the
tool surface does not grow as operations are added.

## JSON contract

```json
{
  "ok": true,
  "data": {},
  "warnings": [],
  "meta": {
    "returned": 50,
    "total": 381,
    "truncated": true,
    "truncated_reason": "row_limit",
    "partial": false,
    "zabbix_version": "7.4.10"
  }
}
```

Errors carry a stable code, whether retrying is worthwhile, and what to do next:

```json
{
  "ok": false,
  "error": {
    "code": "AUTHENTICATION_FAILED",
    "message": "Zabbix rejected the configured API token",
    "retryable": false,
    "suggestion": "run 'zabbix-ai-cli login' to configure a new token"
  }
}
```

`zabbix-ai-cli schema` prints every operation, its parameters and its JSON Schema,
so an agent can learn the tool programmatically instead of guessing at flags.

Full details in [docs/json-output.md](docs/json-output.md).

## Documentation

- [Authentication and profiles](docs/authentication.md)
- [Command line](docs/cli.md)
- [MCP server](docs/mcp.md)
- [Skills](docs/skills.md)
- [JSON output and exit codes](docs/json-output.md)
- [Security model](docs/security.md)
- [Architecture and design decisions](docs/architecture.md)

## Compatibility

Zabbix 6.4 and newer. Bearer-token authentication arrived in 6.4 and is the only
scheme implemented. Version-dependent behaviour is asserted explicitly, so an
incompatibility is reported rather than returning an empty result.

Developed and tested against Zabbix 7.4.

## License

Apache-2.0. See [LICENSE](LICENSE).

Zabbix is a trademark of Zabbix LLC.
This project is an independent open-source project and is not affiliated with or
endorsed by Zabbix LLC.
