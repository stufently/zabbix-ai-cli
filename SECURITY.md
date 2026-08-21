# Security policy

## Reporting a vulnerability

Report privately through GitHub's security advisory form on this repository, or
by email to the maintainer listed there. Please do not open a public issue for a
vulnerability.

Include what you did, what happened, and what you expected. A proof of concept
helps but is not required.

You can expect an acknowledgement within a week and an assessment within two.
Fixes for confirmed issues are released as soon as they are ready, and the
advisory credits the reporter unless asked otherwise.

## Scope

In scope:

- Anything that lets a caller change Zabbix without the approval this program
  requires.
- Any path by which the Zabbix API token reaches stdout, stderr, a log, an error
  message, an audit entry, or an MCP client.
- Escaping the plan directory or the configuration directory through a
  caller-supplied identifier or path.
- Bypassing the risk registry to call a refused API method.
- Authentication or cross-origin weaknesses in the HTTP MCP transport.

Out of scope:

- Vulnerabilities in Zabbix itself. Report those to Zabbix.
- The consequences of a deliberately over-privileged Zabbix token.
- The consequences of `insecure = true` or `--allow-remote`, which exist for
  cases where the operator has decided the trade-off.
- A person approving a change that turned out to be wrong. The approval gate
  exists so a person decides; it does not decide for them.

## Design notes

The threat model assumes the caller may be an AI agent acting on text it read
somewhere — including text this program returns from Zabbix. See
[docs/security.md](docs/security.md) for how each risk is handled.

## Supported versions

The latest release. Security fixes are not backported to earlier tags before 1.0.
