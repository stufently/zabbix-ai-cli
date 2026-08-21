// Package opspec defines operations once, so that the CLI, the MCP server and
// the schema command are three renderings of a single description rather than
// three implementations that drift.
//
// An operation owns its parameters, its JSON Schema, its risk class and the
// scope it needs. Adding a command means adding one value to the registry;
// nothing else has to be kept in step by hand.
package opspec

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stufently/zabbix-ai-cli/internal/config"
	"github.com/stufently/zabbix-ai-cli/internal/errs"
	"github.com/stufently/zabbix-ai-cli/internal/output"
	"github.com/stufently/zabbix-ai-cli/internal/safety"
	"github.com/stufently/zabbix-ai-cli/internal/service"
)

// ParamType is the type of an operation parameter.
type ParamType string

// The parameter types an operation may declare.
const (
	TypeString     ParamType = "string"
	TypeInt        ParamType = "integer"
	TypeBool       ParamType = "boolean"
	TypeDuration   ParamType = "duration"
	TypeStringList ParamType = "string_list"
)

// Param describes one input.
type Param struct {
	Name        string
	Type        ParamType
	Description string
	Required    bool
	// Positional makes this a bare CLI argument rather than a flag.
	Positional bool
	Default    any
	Enum       []string
	Example    string
}

// Operation is one task the tool can perform.
type Operation struct {
	// Name is the canonical dotted identifier, for example "problems.list".
	Name string
	// CLI is the command path, for example {"problems", "list"}.
	CLI []string
	// MCPTool is the tool name exposed over MCP. Empty means the operation is
	// reachable from the command line only.
	MCPTool string
	Summary string
	Long    string
	Risk    safety.Risk
	Scope   string
	Params  []Param

	// Run performs a read and returns a result.
	Run func(ctx context.Context, env *Env, args *Args) (*output.Result, error)
	// Plan describes a change without making it. Write operations set this
	// instead of Run; execution goes through the approval path.
	Plan func(ctx context.Context, env *Env, args *Args) (*safety.Plan, error)
}

// ReadOnly reports whether the operation can change anything.
func (o *Operation) ReadOnly() bool { return o.Risk == safety.RiskRead }

// CommandPath renders the CLI path as a single string.
func (o *Operation) CommandPath() string { return strings.Join(o.CLI, " ") }

// Env is what an operation needs to do its work.
type Env struct {
	Service *service.Service
	Profile string
	Config  config.Profile
	Plans   *safety.Store
	Audit   *safety.AuditLog
	// Limit is the caller's default result bound, applied when an operation
	// declares no limit of its own.
	Limit int
}

// HasScope reports whether the active profile grants the operation's scope.
func (e *Env) HasScope(scope string) bool { return e.Config.HasScope(scope) }

// Args holds validated parameter values.
type Args struct {
	values map[string]any
	params map[string]Param
}

// Raw returns the underlying values, for hashing and audit records.
func (a *Args) Raw() map[string]any { return a.values }

// Has reports whether a value was supplied.
func (a *Args) Has(name string) bool {
	_, ok := a.values[name]
	return ok
}

// String returns a string parameter.
func (a *Args) String(name string) string {
	v, ok := a.values[name]
	if !ok {
		return defaultString(a.params[name])
	}
	s, _ := v.(string)
	return s
}

// Int returns an integer parameter.
func (a *Args) Int(name string) int {
	v, ok := a.values[name]
	if !ok {
		if d, ok := a.params[name].Default.(int); ok {
			return d
		}
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

// Bool returns a boolean parameter.
func (a *Args) Bool(name string) bool {
	v, ok := a.values[name]
	if !ok {
		if d, ok := a.params[name].Default.(bool); ok {
			return d
		}
		return false
	}
	b, _ := v.(bool)
	return b
}

// Duration returns a duration parameter, already parsed.
func (a *Args) Duration(name string) time.Duration {
	v, ok := a.values[name]
	if !ok {
		if d, ok := a.params[name].Default.(string); ok && d != "" {
			parsed, err := service.ParseWindow(d)
			if err == nil {
				return parsed
			}
		}
		return 0
	}
	switch t := v.(type) {
	case time.Duration:
		return t
	case string:
		d, err := service.ParseWindow(t)
		if err != nil {
			return 0
		}
		return d
	default:
		return 0
	}
}

// Strings returns a list parameter.
func (a *Args) Strings(name string) []string {
	v, ok := a.values[name]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return splitList(t)
	default:
		return nil
	}
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func defaultString(p Param) string {
	if s, ok := p.Default.(string); ok {
		return s
	}
	return ""
}

// Bind validates raw input against the operation's parameters.
//
// An unknown parameter is an error that names the accepted ones. Two of the
// nine recorded failures against the tool this replaces were guessed parameter
// names answered with nothing more useful than "unexpected parameter".
func (o *Operation) Bind(input map[string]any) (*Args, error) {
	declared := make(map[string]Param, len(o.Params))
	names := make([]string, 0, len(o.Params))
	for _, p := range o.Params {
		declared[p.Name] = p
		names = append(names, p.Name)
	}
	sort.Strings(names)

	for key := range input {
		if _, ok := declared[key]; !ok {
			return nil, errs.Usage("%s has no parameter %q; it accepts: %s",
				o.CommandPath(), key, strings.Join(names, ", "))
		}
	}

	values := make(map[string]any, len(input))
	for _, p := range o.Params {
		raw, present := input[p.Name]
		if !present || isEmpty(raw) {
			if p.Required {
				return nil, errs.Usage("%s requires %q (%s)", o.CommandPath(), p.Name, p.Description)
			}
			continue
		}
		v, err := coerce(o, p, raw)
		if err != nil {
			return nil, err
		}
		values[p.Name] = v
	}
	return &Args{values: values, params: declared}, nil
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []string:
		return len(t) == 0
	case []any:
		return len(t) == 0
	default:
		return false
	}
}

func coerce(o *Operation, p Param, raw any) (any, error) {
	switch p.Type {
	case TypeString:
		s, ok := stringOf(raw)
		if !ok {
			return nil, errs.Usage("%s: %q must be a string", o.CommandPath(), p.Name)
		}
		if len(p.Enum) > 0 && !containsFold(p.Enum, s) {
			return nil, errs.Usage("%s: %q must be one of: %s", o.CommandPath(), p.Name, strings.Join(p.Enum, ", "))
		}
		return s, nil
	case TypeInt:
		switch t := raw.(type) {
		case int:
			return t, nil
		case int64:
			return int(t), nil
		case float64:
			return int(t), nil
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return nil, errs.Usage("%s: %q must be a whole number", o.CommandPath(), p.Name)
			}
			return n, nil
		default:
			return nil, errs.Usage("%s: %q must be a whole number", o.CommandPath(), p.Name)
		}
	case TypeBool:
		switch t := raw.(type) {
		case bool:
			return t, nil
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(t))
			if err != nil {
				return nil, errs.Usage("%s: %q must be true or false", o.CommandPath(), p.Name)
			}
			return b, nil
		default:
			return nil, errs.Usage("%s: %q must be true or false", o.CommandPath(), p.Name)
		}
	case TypeDuration:
		s, ok := stringOf(raw)
		if !ok {
			return nil, errs.Usage("%s: %q must be a duration such as 30m, 2h or 7d", o.CommandPath(), p.Name)
		}
		d, err := service.ParseWindow(s)
		if err != nil {
			return nil, err
		}
		return d, nil
	case TypeStringList:
		switch t := raw.(type) {
		case []string:
			return t, nil
		case []any:
			out := make([]string, 0, len(t))
			for _, e := range t {
				s, ok := stringOf(e)
				if !ok {
					return nil, errs.Usage("%s: %q must be a list of strings", o.CommandPath(), p.Name)
				}
				out = append(out, s)
			}
			return out, nil
		case string:
			return splitList(t), nil
		default:
			return nil, errs.Usage("%s: %q must be a list of strings", o.CommandPath(), p.Name)
		}
	default:
		return nil, errs.Internal("parameter %q has an unknown type %q", p.Name, p.Type)
	}
}

func stringOf(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case fmt.Stringer:
		return t.String(), true
	default:
		return "", false
	}
}

func containsFold(list []string, v string) bool {
	for _, e := range list {
		if strings.EqualFold(e, v) {
			return true
		}
	}
	return false
}

// InputSchema renders the operation's parameters as JSON Schema, for MCP tool
// registration and for the schema command.
func (o *Operation) InputSchema() map[string]any {
	properties := map[string]any{}
	var required []string
	for _, p := range o.Params {
		prop := map[string]any{"description": p.Description}
		switch p.Type {
		case TypeInt:
			prop["type"] = "integer"
		case TypeBool:
			prop["type"] = "boolean"
		case TypeStringList:
			prop["type"] = "array"
			prop["items"] = map[string]any{"type": "string"}
		case TypeDuration:
			prop["type"] = "string"
			prop["pattern"] = `^[0-9]+(\.[0-9]+)?(s|m|h|d|w)$`
		default:
			prop["type"] = "string"
		}
		if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		if p.Example != "" {
			prop["examples"] = []any{p.Example}
		}
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}

// Describe renders the operation for the schema command.
func (o *Operation) Describe() map[string]any {
	return map[string]any{
		"operation":    o.Name,
		"command":      o.CommandPath(),
		"mcp_tool":     o.MCPTool,
		"description":  o.Summary,
		"details":      o.Long,
		"read_only":    o.ReadOnly(),
		"risk":         string(o.Risk),
		"scope":        o.Scope,
		"input_schema": o.InputSchema(),
	}
}
