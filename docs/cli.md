# Command line

Every command accepts `--json`, `--profile`, `--limit` where it returns a list,
and `--debug` to log API calls on stderr with credentials redacted.

## Reading

```bash
zabbix-ai-cli problems list
zabbix-ai-cli problems list --severity high --since 24h --limit 20
zabbix-ai-cli problems list --host web01 --unacknowledged
zabbix-ai-cli problems get 757474

zabbix-ai-cli host list --search web
zabbix-ai-cli host list "ms*" --monitored
zabbix-ai-cli host get web01
zabbix-ai-cli host status web01
zabbix-ai-cli host investigate web01

zabbix-ai-cli metrics latest web01 --search cpu
zabbix-ai-cli metrics history web01 "cpu util" --last 24h

zabbix-ai-cli triggers list --host web01 --problems
zabbix-ai-cli maintenance list
zabbix-ai-cli unreachable

zabbix-ai-cli alert why 757474
zabbix-ai-cli resolve "$(pbpaste)"
```

Host matching is a case-insensitive substring over both the technical and the
visible name, with exact matches ranked first. `*` is honoured when present.
A pattern matching several hosts is an error listing them, because silently
picking one risks acting on the wrong machine.

## Changing

Write commands describe the change and stop:

```bash
zabbix-ai-cli maintenance create "ms*" --for 2h
```

```
PLAN pl_cc89d2e87d15

Create maintenance "ms* (2h0m)" for 2h0m, ...

Affects:
  host ms1.8qw.ru
  ...

Nothing has changed yet.
To apply it: zabbix-ai-cli approve pl_cc89d2e87d15
```

Add `--apply` to make the change in the same command. Destructive commands also
require `--confirm` naming the target back exactly:

```bash
zabbix-ai-cli maintenance create "ms*" --for 2h --apply
zabbix-ai-cli maintenance extend 42 --by 24h --apply
zabbix-ai-cli maintenance expire 42 --apply
zabbix-ai-cli maintenance delete 42 --apply --confirm "weekend window"

zabbix-ai-cli events acknowledge 757474 --operations ack,message --message "investigating"
zabbix-ai-cli events acknowledge 757474 --operations close --apply --confirm 757474

zabbix-ai-cli triggers disable 35246 --apply --confirm 35246
zabbix-ai-cli triggers enable 35246 --apply
```

Acknowledge operations are named, never numbered: `ack`, `message`, `close`,
`severity`, `unack`, `suppress`, `unsuppress`. The underlying bitmask is easy to
get wrong, and getting it wrong closes a problem that was meant to be commented on.

## Approving

A change requested over MCP arrives as a stored plan:

```bash
zabbix-ai-cli plans list
zabbix-ai-cli plans show pl_cc89d2e87d15
zabbix-ai-cli approve pl_cc89d2e87d15
zabbix-ai-cli reject pl_cc89d2e87d15
```

`approve` prints the plan and asks before doing anything. Plans expire after
fifteen minutes. `--yes` skips the prompt and is required when stdin is not a
terminal, so a non-interactive approval is always deliberate.

## The escape hatch

```bash
zabbix-ai-cli api call host.get --params '{"output":["hostid","host"],"limit":5}'
zabbix-ai-cli api call hostinterface.update --params '{"interfaceid":"402","ip":"10.0.0.9"}' --apply
```

Read methods run immediately. Write methods produce a plan like any other change.
A method absent from the risk registry is refused; `zabbix-ai-cli schema
api-methods` lists the accepted ones with their risk class.

The escape hatch exists because the alternative is worse. The first task the
high-level commands do not cover otherwise gets done with curl and a copied
token, leaving no record at all.

## Profiles

```bash
zabbix-ai-cli profile list
zabbix-ai-cli profile show prod
zabbix-ai-cli profile use prod
zabbix-ai-cli profile scopes prod --add maintenance
zabbix-ai-cli profile delete staging

zabbix-ai-cli --profile staging problems list
```

## Self-description

```bash
zabbix-ai-cli schema
zabbix-ai-cli schema host.investigate
zabbix-ai-cli schema api-methods
```
