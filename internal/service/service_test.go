package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stufently/zabbix-ai-cli/internal/api"
	"github.com/stufently/zabbix-ai-cli/internal/errs"
	"github.com/stufently/zabbix-ai-cli/internal/service"
	"github.com/stufently/zabbix-ai-cli/internal/zbxtest"
)

func newService(t *testing.T, srv *zbxtest.Server) *service.Service {
	t.Helper()
	return service.New(api.New(srv.URL, "test-token"))
}

func TestVersionIsFetchedOnceAndUnauthenticated(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	svc := newService(t, srv)

	for i := 0; i < 3; i++ {
		v, err := svc.Version(context.Background())
		if err != nil {
			t.Fatalf("Version: %v", err)
		}
		if v.String() != "7.4.10" {
			t.Fatalf("version = %q", v.String())
		}
	}
	calls := srv.CallsTo("apiinfo.version")
	if len(calls) != 1 {
		t.Errorf("apiinfo.version was called %d times; it must be cached", len(calls))
	}
	// Zabbix rejects apiinfo.version outright if an authorization header is
	// present, so the client must omit it for this one method.
	if calls[0].Auth != "" {
		t.Errorf("apiinfo.version carried an Authorization header: %q", calls[0].Auth)
	}
}

func TestOtherCallsAreAuthenticated(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{})
	svc := newService(t, srv)
	if _, _, err := svc.ListHosts(context.Background(), service.HostQuery{Limit: 5}); err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	calls := srv.CallsTo("host.get")
	if len(calls) != 1 || calls[0].Auth != "Bearer test-token" {
		t.Fatalf("host.get auth = %q", calls[0].Auth)
	}
}

func TestSearchUsesSubstringMatchingUnlessAWildcardIsGiven(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{zbxtest.Host("1", "web01", nil)})
	svc := newService(t, srv)

	// Zabbix stops applying implicit wildcards once searchWildcardsEnabled is
	// set, which silently turns a substring search into an exact match.
	if _, _, err := svc.ListHosts(context.Background(), service.HostQuery{Search: "web", Limit: 5}); err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	params := srv.CallsTo("host.get")[0].Params
	if _, present := params["searchWildcardsEnabled"]; present {
		t.Error("a plain fragment must not enable wildcard matching")
	}
	if params["searchByAny"] != true {
		t.Error("searchByAny must be set so both name fields are considered")
	}

	srv.Reset()
	if _, _, err := svc.ListHosts(context.Background(), service.HostQuery{Search: "ms*", Limit: 5}); err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	params = srv.CallsTo("host.get")[0].Params
	if params["searchWildcardsEnabled"] != true {
		t.Error("an explicit wildcard must enable wildcard matching")
	}
}

func TestListHostsReportsTruncation(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	rows := make([]any, 0, 4)
	for _, n := range []string{"a", "b", "c", "d"} {
		rows = append(rows, zbxtest.Host(n, "host-"+n, nil))
	}
	srv.Reply("host.get", rows)
	svc := newService(t, srv)

	hosts, truncated, err := svc.ListHosts(context.Background(), service.HostQuery{Limit: 3})
	if err != nil {
		t.Fatalf("ListHosts: %v", err)
	}
	if !truncated {
		t.Error("a full extra row must be reported as truncation")
	}
	if len(hosts) != 3 {
		t.Errorf("returned %d hosts, want the limit of 3", len(hosts))
	}
	// The limit sent to Zabbix asks for one more row than the caller wants, so
	// truncation is detected without a second counting query.
	if got := srv.CallsTo("host.get")[0].Params["limit"]; got != float64(4) {
		t.Errorf("limit sent to Zabbix = %v, want 4", got)
	}
}

func TestResolveHostRejectsAnAmbiguousPattern(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{
		zbxtest.Host("1", "web01-staging", nil),
		zbxtest.Host("2", "web01-backup", nil),
	})
	svc := newService(t, srv)

	_, err := svc.ResolveHost(context.Background(), "web01")
	if err == nil {
		t.Fatal("an ambiguous pattern must not silently pick a host")
	}
	if !strings.Contains(err.Error(), "web01-staging") {
		t.Errorf("the error must list the candidates: %v", err)
	}
}

func TestResolveHostPrefersAnExactMatch(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{
		zbxtest.Host("1", "web01-staging", nil),
		zbxtest.Host("2", "web01", nil),
	})
	svc := newService(t, srv)

	h, err := svc.ResolveHost(context.Background(), "web01")
	if err != nil {
		t.Fatalf("ResolveHost: %v", err)
	}
	if h.Name != "web01" {
		t.Errorf("resolved %q, want the exact match", h.Name)
	}
}

func TestMissingHostSuggestsASearch(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{})
	svc := newService(t, srv)

	_, err := svc.ResolveHost(context.Background(), "nope")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestProblemsResolveHostsThroughTriggers(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("problem.get", []any{
		zbxtest.Problem("100", "500", "Disk full", "4", nil),
	})
	srv.Reply("trigger.get", []any{map[string]any{
		"triggerid": "500",
		"hosts":     []any{map[string]any{"hostid": "10", "name": "db01"}},
	}})
	svc := newService(t, srv)

	problems, _, err := svc.ListProblems(context.Background(), service.ProblemQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListProblems: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("got %d problems", len(problems))
	}
	if len(problems[0].Hosts) != 1 || problems[0].Hosts[0].Name != "db01" {
		t.Errorf("host was not resolved: %+v", problems[0].Hosts)
	}
	// problem.get has no selectHosts parameter; asking for one is an error.
	if _, present := srv.CallsTo("problem.get")[0].Params["selectHosts"]; present {
		t.Error("problem.get must not be sent selectHosts; the API rejects it")
	}
}

func TestSuppressedProblemsAreShownByDefaultAndNamed(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("problem.get", []any{
		zbxtest.Problem("100", "500", "Disk full", "4", map[string]any{
			"suppressed": "1",
			"suppression_data": []any{map[string]any{
				"maintenanceid": "7", "suppress_until": "1790000000", "userid": "0",
			}},
		}),
	})
	srv.Reply("trigger.get", []any{})
	srv.Reply("maintenance.get", []any{map[string]any{"maintenanceid": "7", "name": "planned move"}})
	svc := newService(t, srv)

	problems, _, err := svc.ListProblems(context.Background(), service.ProblemQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListProblems: %v", err)
	}
	if len(problems) != 1 || !problems[0].Suppressed {
		t.Fatalf("suppressed problems must be returned by default: %+v", problems)
	}
	if len(problems[0].SuppressedBy) != 1 || problems[0].SuppressedBy[0].MaintenanceName != "planned move" {
		t.Errorf("the suppressing window must be named: %+v", problems[0].SuppressedBy)
	}
	// Not asking Zabbix to filter is what keeps suppressed problems visible.
	if _, present := srv.CallsTo("problem.get")[0].Params["suppressed"]; present {
		t.Error("the default query must not filter on suppression")
	}
}

func TestExcludeSuppressedIsOptIn(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("problem.get", []any{})
	svc := newService(t, srv)
	if _, _, err := svc.ListProblems(context.Background(),
		service.ProblemQuery{Limit: 10, ExcludeSuppressed: true}); err != nil {
		t.Fatalf("ListProblems: %v", err)
	}
	if got := srv.CallsTo("problem.get")[0].Params["suppressed"]; got != false {
		t.Errorf("suppressed filter = %v, want false", got)
	}
}

func TestLatestValuesQueryHistoryWithTheItemValueType(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})
	srv.Reply("item.get", []any{
		zbxtest.Item("1", "10", "CPU idle", "system.cpu.util[,idle]", "0", map[string]any{"units": "%"}),
		zbxtest.Item("2", "10", "Uptime", "system.uptime", "3", map[string]any{"units": "uptime"}),
	})
	srv.Handle("history.get", func(params map[string]any) (any, error) {
		switch params["history"] {
		case float64(0):
			return []any{map[string]any{"itemid": "1", "clock": nowClock(), "value": "74.16", "ns": "0"}}, nil
		case float64(3):
			return []any{map[string]any{"itemid": "2", "clock": nowClock(), "value": "3600", "ns": "0"}}, nil
		}
		// Zabbix defaults history to 3 and returns nothing useful for a float
		// item; a caller that omits the type sees silence, not an error.
		return []any{}, nil
	})
	svc := newService(t, srv)

	_, values, _, err := svc.LatestValues(context.Background(), service.ItemQuery{Host: "web01", Limit: 10})
	if err != nil {
		t.Fatalf("LatestValues: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("got %d values", len(values))
	}
	byKey := map[string]service.Value{}
	for _, v := range values {
		byKey[v.Key] = v
	}
	if got := byKey["system.cpu.util[,idle]"]; got.Value != "74.16%" || got.NoData {
		t.Errorf("float item = %+v", got)
	}
	if got := byKey["system.uptime"]; got.Value != "1h0m" {
		t.Errorf("uptime formatting = %q", got.Value)
	}
	for _, call := range srv.CallsTo("history.get") {
		if _, present := call.Params["history"]; !present {
			t.Error("history.get was called without an explicit history type")
		}
	}
}

func nowClock() string {
	return itoa64(time.Now().Unix())
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestItemsThatDoNotCollectOnScheduleAreNotCalledStale(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("host.get", []any{zbxtest.Host("10", "web01", nil)})
	srv.Reply("item.get", []any{
		// A trapper item arrives when something sends it; silence is normal.
		zbxtest.Item("1", "10", "Sent metric", "custom.trap", "0", map[string]any{"type": "2", "delay": "0"}),
		zbxtest.Item("2", "10", "Polled metric", "agent.ping", "3", map[string]any{"delay": "1m"}),
	})
	srv.Reply("history.get", []any{})
	svc := newService(t, srv)

	_, values, _, err := svc.LatestValues(context.Background(), service.ItemQuery{Host: "web01", Limit: 10})
	if err != nil {
		t.Fatalf("LatestValues: %v", err)
	}
	for _, v := range values {
		switch v.Key {
		case "custom.trap":
			if v.Stale {
				t.Error("a trapper item with no data must not be reported as stale")
			}
		case "agent.ping":
			if !v.Stale {
				t.Error("a scheduled item with no data must be reported as stale")
			}
		}
	}
}

func TestParseWindowAcceptsOperatorDurations(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
	} {
		got, err := service.ParseWindow(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseWindow(%q) = %v, %v", tc.in, got, err)
		}
	}
	for _, bad := range []string{"", "soon", "-1h", "0s", "5x"} {
		if _, err := service.ParseWindow(bad); err == nil {
			t.Errorf("ParseWindow(%q) must fail", bad)
		}
	}
}

func TestAckMaskIsBuiltFromNamedOperations(t *testing.T) {
	// The 7.4 reference lists "6 - acknowledge event" but its own example
	// computes 34 for acknowledge plus suppress, which is 32 plus 2.
	// Acknowledge is bit 2; assembling the mask from names avoids the trap.
	for _, tc := range []struct {
		ops  []service.AckOperation
		want int
	}{
		{[]service.AckOperation{service.AckAcknowledge}, 2},
		{[]service.AckOperation{service.AckAcknowledge, service.AckMessage}, 6},
		{[]service.AckOperation{service.AckAcknowledge, service.AckSuppress}, 34},
		{[]service.AckOperation{service.AckClose}, 1},
		{[]service.AckOperation{service.AckAcknowledge, service.AckAcknowledge}, 2},
	} {
		got, err := service.AckMask(tc.ops)
		if err != nil {
			t.Errorf("AckMask(%v): %v", tc.ops, err)
			continue
		}
		if got != tc.want {
			t.Errorf("AckMask(%v) = %d, want %d", tc.ops, got, tc.want)
		}
	}
	if _, err := service.AckMask(nil); err == nil {
		t.Error("an empty operation list must fail")
	}
	if _, err := service.AckMask([]service.AckOperation{"explode"}); err == nil {
		t.Error("an unknown operation must fail")
	}
	if _, err := service.AckMask([]service.AckOperation{service.AckAcknowledge, service.AckUnacknowledge}); err == nil {
		t.Error("contradictory operations must fail")
	}
}

func TestSeverityAccepatsNamesAndNumbers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"high", 4}, {"4", 4}, {"disaster", 5}, {"warning", 2}, {"Average", 3},
	} {
		got, err := service.SeverityValue(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("SeverityValue(%q) = %d, %v", tc.in, got, err)
		}
	}
	for _, bad := range []string{"", "9", "urgent"} {
		if _, err := service.SeverityValue(bad); err == nil {
			t.Errorf("SeverityValue(%q) must fail", bad)
		}
	}
}

func TestAuthenticationFailureIsReportedWithoutTheToken(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Fail("host.get", -32602, "Invalid params.", "Not authorised.")
	svc := newService(t, srv)

	_, _, err := svc.ListHosts(context.Background(), service.HostQuery{Limit: 5})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("the error leaked the token: %v", err)
	}
	if !strings.Contains(err.Error(), "rejected the configured API token") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestExpireRefusesAWindowThatHasNotStarted(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	future := time.Now().Add(24 * time.Hour).Unix()
	srv.Reply("maintenance.get", []any{map[string]any{
		"maintenanceid": "7", "name": "planned move", "maintenance_type": "0",
		"active_since": itoa64(future), "active_till": itoa64(future + 7200),
		"hosts": []any{}, "hostgroups": []any{}, "timeperiods": []any{},
	}})
	svc := newService(t, srv)

	// Ending a window that has not begun would otherwise compute an end five
	// minutes after a start that is still in the future, quietly turning the
	// cancellation into a shorter future window.
	_, err := svc.PlanMaintenanceExpire(context.Background(), "prod", "7")
	if err == nil {
		t.Fatal("expiring a window that has not started must be refused")
	}
	var e *errs.E
	if !errors.As(err, &e) || !strings.Contains(e.Suggestion, "delete") {
		t.Errorf("the error should point at deletion instead: %v", err)
	}
}

func TestExpireEndsAnActiveWindow(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	since := time.Now().Add(-2 * time.Hour).Unix()
	srv.Reply("maintenance.get", []any{map[string]any{
		"maintenanceid": "7", "name": "in progress", "maintenance_type": "0",
		"active_since": itoa64(since), "active_till": itoa64(since + 86400),
		"hosts": []any{}, "hostgroups": []any{}, "timeperiods": []any{},
	}})
	svc := newService(t, srv)

	plan, err := svc.PlanMaintenanceExpire(context.Background(), "prod", "7")
	if err != nil {
		t.Fatalf("PlanMaintenanceExpire: %v", err)
	}
	till, ok := plan.Params["active_till"].(int64)
	if !ok {
		t.Fatalf("active_till = %T", plan.Params["active_till"])
	}
	if till > time.Now().Unix()+60 {
		t.Errorf("the window must end now, not in the future: %d", till)
	}
}

func TestHostGroupLookupPrefersAnExactName(t *testing.T) {
	srv := zbxtest.New(t, "7.4.10")
	srv.Reply("hostgroup.get", []any{
		map[string]any{"groupid": "1", "name": "Linux servers"},
		map[string]any{"groupid": "2", "name": "Linux"},
		map[string]any{"groupid": "3", "name": "Embedded Linux"},
	})
	srv.Reply("problem.get", []any{})
	srv.Reply("trigger.get", []any{})
	svc := newService(t, srv)

	if _, _, err := svc.ListProblems(context.Background(),
		service.ProblemQuery{Group: "Linux", Limit: 10}); err != nil {
		t.Fatalf("ListProblems: %v", err)
	}
	// A group literally named "Linux" must not silently widen into every
	// group whose name contains it.
	ids, _ := srv.CallsTo("problem.get")[0].Params["groupids"].([]any)
	if len(ids) != 1 || ids[0] != "2" {
		t.Errorf("groupids = %v, want only the exactly named group", ids)
	}
}

func TestUnixtimeZeroIsNotRenderedAsTheEpoch(t *testing.T) {
	if got := service.FormatValue("0", "unixtime", "3"); got == "1970-01-01T00:00:00Z" {
		t.Errorf("a zero timestamp must not read as a real date: %q", got)
	}
}
