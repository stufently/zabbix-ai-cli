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
- Writes through `api call` are refused for every object whose configuration is
  code or invokes code: `script`, `action`, `mediatype`, `item`, `itemprototype`,
  `discoveryrule`, `hostprototype`, `httptest`, `webscenario`, `connector`,
  `autoregistration`, `proxy` and `proxygroup`. Refusing `script.execute` while
  allowing a script to be created and an action told to run it only lengthened
  the road to running a command on a monitored host. Reading them is unchanged.
- `--http :8000` was treated as loopback because its host part is empty, so it
  bound every interface without requiring `--allow-remote` or a bearer token. An
  empty host now counts as remote, and a host name is loopback only when every
  address it resolves to is.
- A plan's risk and scope were read back from the stored file. They are now
  derived again from the registry when the plan is applied — for `api call`,
  from the method the plan carries — and a plan claiming anything weaker than
  the code says is refused. The hash was never authentication: whoever can
  rewrite the file can recompute it.
- The MCP bearer token can be supplied through `ZABBIX_AI_CLI_MCP_TOKEN`. Passed
  as a flag it is visible to every process on the machine.
- Redaction now covers `passwd`, SNMP community strings and several other
  credential-bearing field names.
- The maintenance skill told the agent reading it that it could apply a plan
  itself with `--apply`. It now says plainly that it cannot.

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
  sample is now spread across the whole list by proportion; the first attempt
  used a whole-number stride, which rounds down to 1 for anything under eighty
  items and quietly became "the first forty" again.
- The exact-name rule for host groups reached only the read paths. `maintenance
  create --groups Linux` still silenced every group containing the word — the
  write path, and the consequential one. Both now share one resolver, which
  looks the exact name up in its own query so a capped substring search cannot
  hide it.
- A plan claimed by an applier reported "no plan exists" to anything that looked
  at it afterwards, rather than saying it was being applied. Claims left behind
  by a killed process are now pruned.
- A negative `limit` meant "no limit". Integer parameters now carry a range,
  publish it in the MCP schema, and refuse a fractional value instead of
  truncating it.
- A failure to write the audit log after an applied change was swallowed. It is
  now reported as a warning on the result.
- `maintenance expire` on a window that started moments ago said alerting
  resumes now, when Zabbix will not end a window shorter than its minimum
  period. The summary says when it actually resumes.
- The container could not start: distroless runs as uid 65532 and
  `/var/lib/zabbix-ai-cli` did not exist. It is created owned by that user, and
  CI now runs the image to check.
- An item with `units: unixtime` and no data rendered as `1970-01-01`.
