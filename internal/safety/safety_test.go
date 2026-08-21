package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPlan(t *testing.T) *Plan {
	t.Helper()
	p, err := NewPlan("maintenance.delete", "prod", RiskDestructive, ScopeMaintenance)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	p.Params = map[string]any{"maintenanceids": []string{"7"}}
	p.Summary = "Delete maintenance"
	if err := p.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return p
}

func TestPlanVerifyDetectsTampering(t *testing.T) {
	p := newPlan(t)
	if err := p.Verify(time.Now()); err != nil {
		t.Fatalf("a fresh plan must verify: %v", err)
	}
	// A plan lives in a file on disk. Editing it must not change what runs.
	p.Params["maintenanceids"] = []string{"8"}
	if err := p.Verify(time.Now()); err == nil {
		t.Fatal("an edited plan must fail verification")
	}
}

func TestPlanExpires(t *testing.T) {
	p := newPlan(t)
	future := time.Now().Add(DefaultTTL + time.Minute)
	if !p.Expired(future) {
		t.Error("a plan must expire")
	}
	if err := p.Verify(future); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("Verify past the deadline = %v", err)
	}
}

func TestPlanIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p := newPlan(t)
		if seen[p.ID] {
			t.Fatalf("duplicate plan id %s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := newPlan(t)
	if err := store.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(p.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Hash != p.Hash || loaded.Summary != p.Summary {
		t.Errorf("round trip changed the plan: %+v", loaded)
	}
	if err := loaded.Verify(time.Now()); err != nil {
		t.Errorf("a stored plan must still verify: %v", err)
	}

	fi, err := os.Stat(filepath.Join(store.Dir(), p.ID+".json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("plan file mode = %04o", perm)
	}
}

func TestStoreRejectsAPathTraversalIdentifier(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, bad := range []string{"../../etc/passwd", "pl_../x", "", "pl_zzz", "pl_0123456789ab/x"} {
		if _, err := store.Load(bad); err == nil {
			t.Errorf("Load(%q) must be refused", bad)
		}
	}
}

func TestListDropsExpiredPlans(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	fresh := newPlan(t)
	stale := newPlan(t)
	stale.ExpiresAt = time.Now().Add(-time.Minute)
	for _, p := range []*Plan{fresh, stale} {
		if err := store.Save(p); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	plans, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != fresh.ID {
		t.Fatalf("List returned %d plans, want only the fresh one", len(plans))
	}
	if _, err := store.Load(stale.ID); err == nil {
		t.Error("an expired plan must be removed from the store")
	}
}

func TestAuditLogRecordsAndFinds(t *testing.T) {
	dir := t.TempDir()
	log, err := NewAuditLog(dir)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	entry := AuditEntry{
		Profile: "prod", Operation: "maintenance.delete", Risk: RiskDestructive,
		PlanID: "pl_0123456789ab", Approval: ApprovalTerminal, Outcome: "applied",
	}
	if err := log.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	found, err := log.Find("pl_0123456789ab")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found == nil || found.Outcome != "applied" || found.Approval != ApprovalTerminal {
		t.Fatalf("Find returned %+v", found)
	}
	missing, err := log.Find("pl_ffffffffffff")
	if err != nil || missing != nil {
		t.Errorf("Find for an unknown plan = %+v, %v", missing, err)
	}

	fi, err := os.Stat(log.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("audit log mode = %04o", perm)
	}
}

func TestClassifyMethod(t *testing.T) {
	for _, tc := range []struct {
		method  string
		risk    Risk
		allowed bool
	}{
		{"host.get", RiskRead, true},
		{"problem.get", RiskRead, true},
		{"apiinfo.version", RiskRead, true},
		{"maintenance.create", RiskWrite, true},
		{"maintenance.delete", RiskDestructive, true},
		{"event.acknowledge", RiskWrite, true},
		{"hostinterface.update", RiskWrite, true},
		{"httptest.update", RiskWrite, true},

		// Refused outright, whatever the profile grants.
		{"script.execute", "", false},
		{"task.create", "", false},
		{"user.login", "", false},
		{"token.generate", "", false},
		{"configuration.export", "", false},
		{"configuration.import", "", false},
		{"settings.update", "", false},
		{"user.delete", "", false},
		{"history.clear", "", false},

		// Not in the registry at all.
		{"nonsense.frobnicate", "", false},
		{"host", "", false},
		{"", "", false},
	} {
		got := ClassifyMethod(tc.method)
		if got.Allowed != tc.allowed {
			t.Errorf("ClassifyMethod(%q).Allowed = %v, want %v (%s)",
				tc.method, got.Allowed, tc.allowed, got.Reason)
			continue
		}
		if tc.allowed && got.Risk != tc.risk {
			t.Errorf("ClassifyMethod(%q).Risk = %q, want %q", tc.method, got.Risk, tc.risk)
		}
	}
}

func TestUnknownMethodsAreDeniedNotGuessedAt(t *testing.T) {
	// Classifying by suffix alone would wave through anything ending in .get
	// and treat every unrecognised write as merely destructive.
	got := ClassifyMethod("madeup.get")
	if got.Allowed {
		t.Error("an unknown object must not be readable")
	}
	if !strings.Contains(got.Reason, "risk registry") {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestKnownMethodsExcludeDeniedOnes(t *testing.T) {
	methods := KnownMethods()
	if len(methods) == 0 {
		t.Fatal("KnownMethods is empty")
	}
	for _, m := range methods {
		if !ClassifyMethod(m).Allowed {
			t.Errorf("KnownMethods lists %q, which is refused", m)
		}
	}
	for _, denied := range []string{"script.execute", "user.login", "configuration.export"} {
		for _, m := range methods {
			if m == denied {
				t.Errorf("KnownMethods must not list the refused method %q", denied)
			}
		}
	}
}
