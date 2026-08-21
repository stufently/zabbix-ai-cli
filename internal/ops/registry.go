// Package ops is the registry of everything zabbix-ai-cli can do.
//
// Each operation appears exactly once. The CLI builds commands from this list,
// the MCP server builds tools from it, and `schema` prints it, so a change to
// an operation reaches all three at once.
package ops

import (
	"sort"
	"strings"

	"github.com/stufently/zabbix-ai-cli/internal/errs"
	"github.com/stufently/zabbix-ai-cli/internal/opspec"
	"github.com/stufently/zabbix-ai-cli/internal/safety"
)

var registry []*opspec.Operation

func register(ops ...*opspec.Operation) {
	registry = append(registry, ops...)
}

// All returns every operation, ordered by command path.
func All() []*opspec.Operation {
	out := make([]*opspec.Operation, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].CommandPath() < out[j].CommandPath() })
	return out
}

// Lookup finds an operation by its canonical name.
func Lookup(name string) (*opspec.Operation, bool) {
	for _, o := range registry {
		if o.Name == name {
			return o, true
		}
	}
	return nil, false
}

// LookupCommand finds an operation by its command path, for the schema
// command, which accepts either form.
func LookupCommand(path string) (*opspec.Operation, bool) {
	path = strings.TrimSpace(path)
	for _, o := range registry {
		if o.CommandPath() == path || o.Name == path {
			return o, true
		}
	}
	return nil, false
}

// Writable returns the operations that change something, which is the set a
// plan may name.
func Writable() []*opspec.Operation {
	var out []*opspec.Operation
	for _, o := range All() {
		if o.Plan != nil {
			out = append(out, o)
		}
	}
	return out
}

// WritableNames lists the writable operations, for the plan tool's enum and
// for error messages.
func WritableNames() []string {
	ops := Writable()
	names := make([]string, 0, len(ops))
	for _, o := range ops {
		names = append(names, o.Name)
	}
	return names
}

// CheckScope reports whether a profile may plan the operation at all.
//
// This is a second boundary behind the permissions of the Zabbix token: a
// read-only profile cannot produce a plan even for an operation the token
// would be allowed to perform.
func CheckScope(env *opspec.Env, o *opspec.Operation) error {
	if o.Risk == safety.RiskRead {
		return nil
	}
	if env.HasScope(o.Scope) {
		return nil
	}
	return errs.New(errs.CodeScope, errs.ExitPermission,
		"profile %q does not grant the %q scope, which %s requires", env.Profile, o.Scope, o.Name).
		WithSuggestion("add it with 'zabbix-ai-cli profile scopes %s --add %s'", env.Profile, o.Scope)
}
