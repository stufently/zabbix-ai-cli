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
