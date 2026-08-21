package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/zabbix-ai-cli/internal/api"
	"github.com/stufently/zabbix-ai-cli/internal/config"
	zmcp "github.com/stufently/zabbix-ai-cli/internal/mcp"
	"github.com/stufently/zabbix-ai-cli/internal/opspec"
	"github.com/stufently/zabbix-ai-cli/internal/safety"
	"github.com/stufently/zabbix-ai-cli/internal/service"
	"github.com/stufently/zabbix-ai-cli/internal/zbxtest"
)

const testToken = "mcp-secret-token"

type harness struct {
	server  *zbxtest.Server
	session *sdk.ClientSession
	plans   *safety.Store
}

func newHarness(t *testing.T, readOnly bool, scopes ...string) *harness {
	t.Helper()
	srv := zbxtest.New(t, "7.4.10")
	stateDir := t.TempDir()
	plans, err := safety.NewStore(stateDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	audit, err := safety.NewAuditLog(stateDir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}

	server := zmcp.NewServer(zmcp.Options{
		Version:  "test",
		ReadOnly: readOnly,
		EnvFor: func(context.Context) (*opspec.Env, error) {
			return &opspec.Env{
				Service: service.New(api.New(srv.URL, testToken)),
				Profile: "test",
				Config:  config.Profile{URL: srv.URL, Scopes: scopes},
				Plans:   plans,
				Audit:   audit,
			}, nil
		},
	})

	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &harness{server: srv, session: session, plans: plans}
}

func (h *harness) call(t *testing.T, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: name, Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func text(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func envelope(t *testing.T, res *sdk.CallToolResult) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(text(t, res)), &env); err != nil {
		t.Fatalf("tool output is not JSON: %v\n%s", err, text(t, res))
	}
	return env
}

func TestToolSurfaceIsSmallAndNamespaced(t *testing.T) {
	h := newHarness(t, false)
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
		if !strings.HasPrefix(tool.Name, "zabbix_") {
			t.Errorf("tool %q is not namespaced", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
	// A large tool surface costs an agent context before it does anything at
	// all; the whole point of this design is a curated handful.
	if len(res.Tools) > 20 {
		t.Errorf("the server exposes %d tools; the design calls for a small set", len(res.Tools))
	}
	for _, required := range []string{
		"zabbix_problems", "zabbix_hosts", "zabbix_host_investigate",
		"zabbix_alert_why", "zabbix_resolve", "zabbix_api_call",
		"zabbix_plan_create", "zabbix_plan_status",
	} {
		if !names[required] {
			t.Errorf("tool %q is missing", required)
		}
	}
}

func TestNoToolCanChangeZabbix(t *testing.T) {
	h := newHarness(t, false, config.ScopeMaintenance)
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// Every tool is annotated read-only because none of them writes: the
	// planning tool only records an intention for a person to approve.
	for _, tool := range res.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q is not annotated as read-only", tool.Name)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		for _, forbidden := range []string{`"apply"`, `"confirm"`, `"force"`} {
			if strings.Contains(string(schema), forbidden) {
				t.Errorf("tool %q accepts %s; MCP clients must not be able to authorise a change",
					tool.Name, forbidden)
			}
		}
	}
}

func TestReadToolReturnsTheEnvelope(t *testing.T) {
	h := newHarness(t, false)
	h.server.Reply("problem.get", []any{zbxtest.Problem("100", "500", "Disk full", "4", nil)})
	h.server.Reply("trigger.get", []any{map[string]any{
		"triggerid": "500", "hosts": []any{map[string]any{"hostid": "10", "name": "db01"}},
	}})

	res := h.call(t, "zabbix_problems", map[string]any{"limit": 10})
	if res.IsError {
		t.Fatalf("tool reported an error: %s", text(t, res))
	}
	env := envelope(t, res)
	if env["ok"] != true {
		t.Fatalf("ok = %v", env["ok"])
	}
	if res.StructuredContent == nil {
		t.Error("structured content is missing")
	}
}

func TestUnknownParameterIsRefusedWithTheAcceptedOnes(t *testing.T) {
	h := newHarness(t, false)
	res := h.call(t, "zabbix_problems", map[string]any{"selectHosts": "extend"})
	if !res.IsError {
		t.Fatal("an unknown parameter must be refused")
	}
	body := text(t, res)
	if !strings.Contains(body, "severity") || !strings.Contains(body, "selectHosts") {
		t.Errorf("the error must name the accepted parameters: %s", body)
	}
}

func TestPlanToolDescribesButDoesNotApply(t *testing.T) {
	h := newHarness(t, false, config.ScopeMaintenance)
	h.server.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})
	h.server.Reply("maintenance.create", map[string]any{"maintenanceids": []any{"1"}})

	res := h.call(t, "zabbix_plan_create", map[string]any{
		"operation": "maintenance.create",
		"params":    map[string]any{"hosts": []any{"web01"}, "for": "2h"},
	})
	if res.IsError {
		t.Fatalf("plan creation failed: %s", text(t, res))
	}
	data := envelope(t, res)["data"].(map[string]any)
	if data["status"] != "planned" {
		t.Errorf("status = %v", data["status"])
	}
	approve, _ := data["approve_command"].(string)
	if !strings.HasPrefix(approve, "zabbix-ai-cli approve pl_") {
		t.Errorf("approve command = %q", approve)
	}
	if calls := h.server.CallsTo("maintenance.create"); len(calls) != 0 {
		t.Fatal("the planning tool changed Zabbix")
	}
}

func TestPlanToolRefusesAnOperationOutsideTheProfileScope(t *testing.T) {
	h := newHarness(t, false) // read-only profile
	h.server.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})

	res := h.call(t, "zabbix_plan_create", map[string]any{
		"operation": "maintenance.create",
		"params":    map[string]any{"hosts": []any{"web01"}, "for": "2h"},
	})
	if !res.IsError {
		t.Fatal("a read-only profile must not be able to plan a write")
	}
	if !strings.Contains(text(t, res), "SCOPE_NOT_GRANTED") {
		t.Errorf("error = %s", text(t, res))
	}
}

func TestPlanToolIsAbsentInReadOnlyMode(t *testing.T) {
	h := newHarness(t, true, config.ScopeMaintenance)
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if strings.HasPrefix(tool.Name, "zabbix_plan") {
			t.Errorf("read-only mode still exposes %q", tool.Name)
		}
	}
}

func TestPlanStatusReportsTheOutcome(t *testing.T) {
	h := newHarness(t, false, config.ScopeMaintenance)
	h.server.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})

	created := h.call(t, "zabbix_plan_create", map[string]any{
		"operation": "maintenance.create",
		"params":    map[string]any{"hosts": []any{"web01"}, "for": "2h"},
	})
	planID := envelope(t, created)["data"].(map[string]any)["plan_id"].(string)

	res := h.call(t, "zabbix_plan_status", map[string]any{"plan_id": planID})
	data := envelope(t, res)["data"].(map[string]any)
	if data["status"] != "awaiting approval" {
		t.Errorf("status = %v", data["status"])
	}

	res = h.call(t, "zabbix_plan_status", map[string]any{"plan_id": "pl_ffffffffffff"})
	data = envelope(t, res)["data"].(map[string]any)
	if data["status"] != "gone" {
		t.Errorf("status for an unknown plan = %v", data["status"])
	}
}

func TestApiCallToolIsReadOnly(t *testing.T) {
	h := newHarness(t, false, config.ScopeMaintenance)
	h.server.Reply("host.get", []any{map[string]any{"hostid": "1"}})

	res := h.call(t, "zabbix_api_call", map[string]any{"method": "host.get"})
	if res.IsError {
		t.Fatalf("a read method must work: %s", text(t, res))
	}

	res = h.call(t, "zabbix_api_call", map[string]any{
		"method": "maintenance.delete", "params": `["7"]`,
	})
	if !res.IsError {
		t.Fatal("the escape hatch must not perform writes over MCP")
	}
	if !strings.Contains(text(t, res), "zabbix_plan_create") {
		t.Errorf("the refusal should point at the planning tool: %s", text(t, res))
	}
	if calls := h.server.CallsTo("maintenance.delete"); len(calls) != 0 {
		t.Fatal("a write reached Zabbix through the escape hatch")
	}
}

func TestToolOutputNeverCarriesTheToken(t *testing.T) {
	h := newHarness(t, false, config.ScopeMaintenance)
	h.server.Reply("problem.get", []any{})
	h.server.Reply("trigger.get", []any{})
	h.server.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})

	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"zabbix_problems", map[string]any{}},
		{"zabbix_hosts", map[string]any{}},
		{"zabbix_api_call", map[string]any{"method": "host.get"}},
		{"zabbix_plan_create", map[string]any{
			"operation": "maintenance.create",
			"params":    map[string]any{"hosts": []any{"web01"}, "for": "2h"},
		}},
	} {
		res := h.call(t, call.name, call.args)
		if strings.Contains(text(t, res), testToken) {
			t.Errorf("%s leaked the Zabbix token to the MCP client", call.name)
		}
	}
}

func TestErrorsAreReportedInsideTheResult(t *testing.T) {
	h := newHarness(t, false)
	h.server.Fail("problem.get", -32602, "Invalid params.", "Not authorised.")

	res := h.call(t, "zabbix_problems", map[string]any{})
	if !res.IsError {
		t.Fatal("a failure must be marked as an error result")
	}
	env := envelope(t, res)
	if env["ok"] != false {
		t.Errorf("ok = %v", env["ok"])
	}
	body := env["error"].(map[string]any)
	if body["code"] != "AUTHENTICATION_FAILED" {
		t.Errorf("code = %v", body["code"])
	}
}
