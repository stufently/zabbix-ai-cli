# Changelog

## [Unreleased]

### Added

- 2026-08-21 — First working version.
  - Single Go binary providing a CLI, an MCP server and five agent skills over one
    operation registry, so the command line and the MCP tools cannot diverge.
  - Read commands: `problems list|get`, `host list|get|status|investigate`,
    `metrics latest|history`, `triggers list`, `maintenance list`, `unreachable`,
    `alert why`, `resolve`, `api call`.
  - Write operations under plan and approval: `maintenance create|extend|expire|delete`,
    `events acknowledge`, `triggers disable|enable`, `api call --apply`.
  - Safety model: an explicit risk registry, profile scopes, plans with hashed
    parameters, a fifteen-minute deadline, preconditions re-checked against live
    Zabbix, terminal-only approval for anything requested over MCP, and an audit
    log of every applied change.
  - Thirteen MCP tools over stdio and streamable HTTP, with cross-origin
    protection and constant-time bearer authentication on the HTTP transport.
  - Stable JSON envelope with truncation and partial-result metadata, nine
    documented exit codes, and `schema` generated from the same registry.
  - Credential resolution across stdin, environment, token file, OS keyring and a
    credentials file, with no silent downgrade from the keyring to plain text.

### Security

Found by independent review of the finished implementation and fixed before release:

- A plan's fingerprint covered only its operation and parameters, leaving the
  deadline, risk class, required confirmation, preconditions and summary
  editable on disk. Plans are files owned by the same user an agent runs as, so
  the whole plan is now fingerprinted — including the summary, because that is
  the text a person reads before approving.
- `api call` declares itself a read, because whether it writes depends on the
  method it is handed. The scope check keyed on that declaration, so a
  read-only profile could plan and apply `maintenance.delete` through the escape
  hatch. Scope is now checked against the plan that was actually built.
- Two concurrent approvals of the same plan could both reach Zabbix. A plan is
  now claimed by an atomic rename before anything is sent, and discarded
  afterwards whether or not the change succeeded.
- `usermacro.get` is refused. `configuration.export` was already refused because
  its output embeds macros; reading those macros directly is the same secrets by
  another route, and this tool's output lands in a model's context.

### Fixed

Behaviours found against a live Zabbix 7.4.10 server, each of which produces a
plausible wrong answer rather than an error:

- `searchWildcardsEnabled: true` disables implicit substring matching, so name
  fragments matched nothing. Wildcards are now enabled only when the pattern
  contains one.
- `history.get` defaults to the numeric-unsigned table and returns nothing for a
  float item. Value types are read first and item IDs grouped per type.
- `item.lastvalue` and `item.lastclock` return a constant `"0"`. Latest values
  come from history.
- `problem.get` has no `selectHosts`. Hosts are resolved through a batched
  `trigger.get`.
- `host.available` no longer exists; availability is read per interface, plus
  `active_available`.
- `apiinfo.version` is rejected when an Authorization header is present.
- `event.acknowledge` takes a bitmask whose documentation contradicts its own
  example. Operations are named and the mask assembled internally.

Also found by review:

- `maintenance expire` on a window that had not yet started moved its end to
  five minutes after a start still in the future, which looked like a
  cancellation and was not one. It is now refused, pointing at `delete`.
- A host group named exactly `Linux` silently widened into every group whose
  name contained it. An exact name now wins outright.
- No-data detection sampled the first forty items, which are alphabetical. The
  sample is now strided across the whole list.
- An item with `units: unixtime` and no data rendered as `1970-01-01`.
