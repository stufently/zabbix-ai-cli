# Security model

## Threat model

The distinguishing assumption is that the caller may be an AI agent acting on
instructions it read somewhere. Those instructions may come from a ticket, a log
line, or a Zabbix host name — all of which this program returns.

| Risk | Mitigation |
| --- | --- |
| An agent makes a destructive change it was talked into | No MCP tool can write. A change becomes a plan; a person applies it at a terminal. |
| Prompt injection supplies a confirmation | The confirmation does not exist in the model's context. There is no MCP parameter that authorises anything. |
| An agent acts on a stale plan | Parameters are hashed, plans expire after 15 minutes, and preconditions are re-read from Zabbix immediately before execution. |
| The token leaks into output or logs | The token is never printed, never logged, and never included in an error. Debug logging redacts request bodies. The MCP client never receives it. |
| Injection through Zabbix data | Returned strings are stripped of control characters and length-bounded. No policy decision reads their content. |
| An unclassified API method does something unexpected | The risk registry is an explicit table. A method absent from it is refused, not guessed at. |
| A single query exhausts the agent's context | Every list bounds itself and reports truncation. Field projections replace `output: extend`. |
| Shell injection | Nothing in this program builds a shell command. Parameters travel as JSON to the Go HTTP client. |
| An exposed MCP endpoint | HTTP binds loopback unless overridden, requires a bearer token off loopback, compares it in constant time, and applies cross-origin protection. |
| A downgraded TLS connection | Verification is on by default. A private CA is configured with `ca_file`; `insecure` exists but is never a default. |

## Why writes are permitted at all

Refusing writes outright is tempting and, in this case, was tried. What happened
was that the work got done anyway — with a token read out of a container's
environment, through a script that logged nothing. The refusal did not remove the
capability, only the record of it.

A permitted path that is planned, approved by a person and written to an audit log
is a better outcome than a refusal that gets routed around.

## What is refused outright

Some methods are refused whatever a profile grants, because no diagnostic
workflow needs them and each one either hands out a credential, executes code, or
moves data that carries credentials:

`script.execute`, `task.create`, `user.login`, `token.*`, `configuration.export`,
`configuration.import`, `authentication.update`, `settings.update`,
`history.clear`, and user, group, role and directory administration.

`configuration.export` deserves a note: it reads, so a classifier that keys on the
verb would wave it through. Its output embeds macros, which is where Zabbix
installations keep passwords.

## Layers

1. **The Zabbix token's own permissions.** The last real boundary. Give the token
   the least privilege the work needs; nothing here can widen it.
2. **Profile scopes.** A profile grants `read` and nothing else until told
   otherwise. Without the matching scope, a write cannot even be planned.
3. **The risk registry.** An explicit table of what each operation may do.
4. **Plan and approval.** Nothing writes on the call that requested it.
5. **The audit log.** Every applied change, with its plan, its parameters
   redacted of secrets, and how it was authorised.

## Platform note

File permissions are the enforcement mechanism for the configuration, the
credentials file, stored plans and the audit log: directories `0700`, files
`0600`, with owner and symlink checks before a credential is read.

On Windows those mode bits are not enforced by the operating system, and Go's
`os.Chmod` cannot express an ACL. On a shared Windows machine, protect
`%AppData%\zabbix-ai-cli` through the filesystem ACL, or supply the token
through the environment and keep nothing on disk.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
