package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryEmbeddedSkillIsUsable(t *testing.T) {
	list, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 3 {
		t.Fatalf("only %d skills are embedded", len(list))
	}
	for _, s := range list {
		if !strings.HasPrefix(s.Name, "zabbix-") {
			t.Errorf("skill %q is not namespaced", s.Name)
		}
		// The description is what an agent matches against when deciding
		// whether a skill applies, so an empty one makes the skill invisible.
		if len(s.Description) < 40 {
			t.Errorf("skill %q has a description too short to be matched on: %q", s.Name, s.Description)
		}
	}
}

func TestInstallCopiesAndDoesNotOverwrite(t *testing.T) {
	dest := t.TempDir()
	results, err := Install(dest, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, r := range results {
		if r.Status != "installed" {
			t.Errorf("%s: %s", r.Name, r.Status)
		}
		if _, err := os.Stat(filepath.Join(r.Path, "SKILL.md")); err != nil {
			t.Errorf("%s: SKILL.md missing: %v", r.Name, err)
		}
	}

	// A second run must leave an edited skill alone: overwriting the user's
	// work without being asked is not this program's call to make.
	edited := filepath.Join(results[0].Path, "SKILL.md")
	if err := os.WriteFile(edited, []byte("edited by the user"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := Install(dest, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(again[0].Status, "skipped") {
		t.Errorf("status = %q, want a skip", again[0].Status)
	}
	data, _ := os.ReadFile(edited)
	if string(data) != "edited by the user" {
		t.Error("an existing skill was overwritten without --force")
	}

	if _, err := Install(dest, true); err != nil {
		t.Fatalf("Install with force: %v", err)
	}
	data, _ = os.ReadFile(edited)
	if string(data) == "edited by the user" {
		t.Error("--force did not overwrite")
	}
}

func TestDestinationsMatchTheRuntimeConventions(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetCodex} {
		global, err := Destination(target, true)
		if err != nil {
			t.Fatalf("Destination(%s): %v", target, err)
		}
		if !strings.HasSuffix(global, filepath.Join("skills")) {
			t.Errorf("%s global destination = %q", target, global)
		}
		project, err := Destination(target, false)
		if err != nil {
			t.Fatalf("Destination(%s, project): %v", target, err)
		}
		if project == global {
			t.Errorf("%s: project and user destinations must differ", target)
		}
	}
	if _, err := Destination("emacs", true); err == nil {
		t.Error("an unknown runtime must be refused")
	}
}
