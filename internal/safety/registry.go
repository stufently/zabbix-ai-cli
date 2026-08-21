package safety

import (
	"sort"
	"strings"
)

// Classification is the verdict of the risk registry for one raw API method.
type Classification struct {
	Risk  Risk
	Scope string
	// Allowed is false for a method this program refuses to call at all.
	Allowed bool
	// Reason explains a refusal.
	Reason string
}

// Scope names, matching the values a profile may grant.
const (
	ScopeRead          = "read"
	ScopeAcknowledge   = "acknowledge"
	ScopeMaintenance   = "maintenance"
	ScopeConfiguration = "configuration"
)

// deniedMethods are refused regardless of profile scope.
//
// Each entry either hands out a credential, executes code, or moves data that
// carries credentials. None of them belongs in a path an agent can reach, and
// none of them is worth the risk of a heuristic getting it wrong.
var deniedMethods = map[string]string{
	"user.login":            "this program authenticates with an API token; a session login would create a second credential",
	"user.logout":           "session logins are not used",
	"token.create":          "creating API tokens from here would let an agent mint its own credentials",
	"token.update":          "modifying API tokens would let an agent widen its own access",
	"token.generate":        "generating API token values would expose a credential",
	"token.delete":          "deleting API tokens can lock out other integrations",
	"script.execute":        "this runs arbitrary commands on monitored hosts",
	"task.create":           "tasks execute checks and scripts on demand",
	"configuration.export":  "exports embed macros and credentials in plain text",
	"configuration.import":  "imports rewrite templates and hosts wholesale",
	"authentication.update": "this changes how every user signs in",
	"settings.update":       "this changes installation-wide behaviour",
	"user.create":           "user administration is outside this tool's remit",
	"user.update":           "user administration is outside this tool's remit",
	"user.delete":           "user administration is outside this tool's remit",
	"usergroup.create":      "user administration is outside this tool's remit",
	"usergroup.update":      "user administration is outside this tool's remit",
	"usergroup.delete":      "user administration is outside this tool's remit",
	"role.create":           "role administration is outside this tool's remit",
	"role.update":           "role administration is outside this tool's remit",
	"role.delete":           "role administration is outside this tool's remit",
	"user.unblock":          "user administration is outside this tool's remit",
	"user.provision":        "user administration is outside this tool's remit",
	"user.resettotp":        "user administration is outside this tool's remit",
	"userdirectory.create":  "directory administration is outside this tool's remit",
	"userdirectory.update":  "directory administration is outside this tool's remit",
	"userdirectory.delete":  "directory administration is outside this tool's remit",
	"mfa.create":            "authentication administration is outside this tool's remit",
	"mfa.update":            "authentication administration is outside this tool's remit",
	"mfa.delete":            "authentication administration is outside this tool's remit",
	"history.clear":         "this permanently deletes collected measurements, and nothing in a diagnostic workflow needs to",
	"usermacro.get":         "macro values are where installations keep database passwords and API keys, and this tool's output goes into a model's context",
}

// readOnlyObjects are the API objects whose .get method this program will call
// through the raw escape hatch.
var readOnlyObjects = []string{
	"action", "alert", "api", "auditlog", "autoregistration", "configuration",
	"connector", "correlation", "dashboard", "dhost", "discoveryrule", "drule",
	"dservice", "event", "graph", "graphitem", "graphprototype", "hanode",
	"history", "host", "hostgroup", "hostinterface", "hostprototype", "housekeeping",
	"httptest", "iconmap", "image", "item", "itemprototype", "maintenance", "map",
	"mediatype", "problem", "proxy", "proxygroup", "regexp", "report", "role",
	"script", "service", "sla", "task", "template", "templatedashboard",
	"templategroup", "token", "trend", "trigger", "triggerprototype", "user",
	"userdirectory", "usergroup", "usermacro", "valuemap", "webscenario",
}

// writeObjects maps an object to the scope its writes require. An object
// absent from this map cannot be written through the escape hatch.
var writeObjects = map[string]string{
	"maintenance":      ScopeMaintenance,
	"event":            ScopeAcknowledge,
	"host":             ScopeConfiguration,
	"hostgroup":        ScopeConfiguration,
	"hostinterface":    ScopeConfiguration,
	"trigger":          ScopeConfiguration,
	"triggerprototype": ScopeConfiguration,
	"template":         ScopeConfiguration,
	"templategroup":    ScopeConfiguration,
	"usermacro":        ScopeConfiguration,
	"valuemap":         ScopeConfiguration,
	"service":          ScopeConfiguration,
	"sla":              ScopeConfiguration,
	"dashboard":        ScopeConfiguration,
	"correlation":      ScopeConfiguration,
	"graph":            ScopeConfiguration,
	"graphprototype":   ScopeConfiguration,
	"regexp":           ScopeConfiguration,
	"iconmap":          ScopeConfiguration,
	"image":            ScopeConfiguration,
	"map":              ScopeConfiguration,
	"report":           ScopeConfiguration,
	"housekeeping":     ScopeConfiguration,
}

// executableObjects are objects whose configuration is code, or invokes code.
//
// Denying script.execute closes the short road to running a command on a
// monitored host. These are the long ones: a script plus an action that runs
// it, an item of type SSH, Telnet, script or browser, a script media type that
// fires on every alert, a connector that streams the installation's data to an
// endpoint of the author's choosing. Each is a legitimate part of Zabbix and
// none of it belongs behind a generic escape hatch, so they are refused
// outright rather than left behind a scope a profile might hold.
var executableObjects = map[string]string{
	"script":           "a script is a command definition, and an action can run it without script.execute ever being called",
	"action":           "action operations run scripts and remote commands on hosts",
	"mediatype":        "a script media type executes a program on the Zabbix server for every alert it sends",
	"item":             "SSH, Telnet, script and browser items execute code every time they collect",
	"itemprototype":    "item prototypes become items, and items can execute code",
	"discoveryrule":    "a discovery rule creates items from its prototypes",
	"hostprototype":    "host prototypes carry the items discovery creates",
	"httptest":         "a web scenario makes the Zabbix server issue requests of the author's choosing",
	"webscenario":      "a web scenario makes the Zabbix server issue requests of the author's choosing",
	"connector":        "a connector streams monitoring data to an external endpoint",
	"autoregistration": "autoregistration decides what happens to every new host that appears",
	"proxy":            "a proxy collects for the hosts assigned to it, and its address decides where they report",
	"proxygroup":       "proxy groups decide which proxy collects for which hosts",
}

var writeActions = map[string]bool{
	"create": true, "update": true, "massadd": true, "massupdate": true,
	"massremove": true, "replacehostinterfaces": true, "copy": true,
	"acknowledge": true, "createglobal": true, "updateglobal": true,
	"adddependencies": true, "deletedependencies": true, "propertyupdate": true,
}

var destructiveActions = map[string]bool{
	"delete": true, "deleteglobal": true, "clear": true,
}

// alwaysRead are exact methods that read despite not ending in ".get".
var alwaysRead = map[string]bool{
	"apiinfo.version":             true,
	"user.checkauthentication":    false,
	"script.getscriptsbyhosts":    true,
	"script.getscriptsbyevents":   true,
	"configuration.importcompare": false,
}

// ClassifyMethod decides what a raw Zabbix API method may do.
//
// The table is explicit on purpose. Classifying by suffix alone would call
// script.execute an ordinary read and task.create an ordinary write, and an
// unrecognised method would be waved through on the strength of its name.
// Anything not listed here is refused.
func ClassifyMethod(method string) Classification {
	m := strings.ToLower(strings.TrimSpace(method))
	if reason, denied := deniedMethods[m]; denied {
		return Classification{Allowed: false, Reason: reason}
	}
	if allowed, ok := alwaysRead[m]; ok {
		if !allowed {
			return Classification{Allowed: false, Reason: "this method is not part of the supported surface"}
		}
		return Classification{Risk: RiskRead, Scope: ScopeRead, Allowed: true}
	}
	object, action, ok := strings.Cut(m, ".")
	if !ok || object == "" || action == "" {
		return Classification{Allowed: false,
			Reason: "a Zabbix API method looks like object.action, for example host.get"}
	}
	if reason, executable := executableObjects[object]; executable && action != "get" {
		return Classification{Allowed: false, Reason: reason}
	}
	switch {
	case action == "get":
		if contains(readOnlyObjects, object) {
			return Classification{Risk: RiskRead, Scope: ScopeRead, Allowed: true}
		}
	case destructiveActions[action]:
		if scope, ok := writeObjects[object]; ok {
			return Classification{Risk: RiskDestructive, Scope: scope, Allowed: true}
		}
	case writeActions[action]:
		if scope, ok := writeObjects[object]; ok {
			return Classification{Risk: RiskWrite, Scope: scope, Allowed: true}
		}
	}
	return Classification{Allowed: false,
		Reason: "this method is not in the risk registry, and an unclassified method is refused rather than guessed at"}
}

// KnownMethods lists every method the escape hatch accepts, for the schema
// command and for error messages that would otherwise leave a caller guessing.
func KnownMethods() []string {
	set := map[string]bool{"apiinfo.version": true, "script.getscriptsbyhosts": true, "script.getscriptsbyevents": true}
	for _, o := range readOnlyObjects {
		set[o+".get"] = true
	}
	for o := range writeObjects {
		for a := range writeActions {
			set[o+"."+a] = true
		}
		for a := range destructiveActions {
			set[o+"."+a] = true
		}
	}
	for m := range deniedMethods {
		delete(set, m)
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}
