package safety

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/stufently/zabbix-ai-cli/internal/errs"
)

// planIDPattern constrains an identifier to what this program generates, so a
// caller-supplied value can never escape the plan directory.
var planIDPattern = regexp.MustCompile(`^pl_[0-9a-f]{12}$`)

// Store persists plans between the call that creates one and the terminal
// session that approves it.
type Store struct{ dir string }

// NewStore prepares the plan directory.
func NewStore(stateDir string) (*Store, error) {
	dir := filepath.Join(stateDir, "plans")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Dir reports the plan directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(id string) (string, error) {
	if !planIDPattern.MatchString(id) {
		return "", errs.Usage("%q is not a plan identifier; they look like pl_1a2b3c4d5e6f", id)
	}
	return filepath.Join(s.dir, id+".json"), nil
}

// Save writes a plan.
func (s *Store) Save(p *Plan) error {
	path, err := s.path(p.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".plan-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Load reads a plan by identifier.
func (s *Store) Load(id string) (*Plan, error) {
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, errs.New(errs.CodePlanNotFound, errs.ExitNotFound, "no plan %s exists", id).
			WithSuggestion("run 'zabbix-ai-cli plans list' to see outstanding plans; they expire after %s", DefaultTTL)
	}
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errs.Internal("plan %s is corrupt: %v", id, err)
	}
	return &p, nil
}

// List returns outstanding plans, newest first, discarding expired ones as it
// goes so the directory does not accumulate.
func (s *Store) List() ([]*Plan, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var plans []*Plan
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := s.Load(id)
		if err != nil {
			continue
		}
		if p.Expired(now) {
			_ = s.Delete(id)
			continue
		}
		plans = append(plans, p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].CreatedAt.After(plans[j].CreatedAt) })
	return plans, nil
}

// Delete removes a plan. A missing plan is not an error.
func (s *Store) Delete(id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// claimedSuffix marks a plan that an applier has taken ownership of.
const claimedSuffix = ".claimed"

// Claim takes exclusive ownership of a plan by renaming its file.
//
// Rename is atomic, so of two concurrent approvals exactly one succeeds and
// the other is refused. Without it both could load the plan, pass every check
// and reach Zabbix before either removed the file — applying the same change
// twice.
func (s *Store) Claim(id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Rename(path, path+claimedSuffix); err != nil {
		if os.IsNotExist(err) {
			return errs.New(errs.CodePlanNotFound, errs.ExitNotFound,
				"plan %s is already being applied, or has been applied or rejected", id).
				WithSuggestion("run the original command again to build a fresh plan")
		}
		return err
	}
	return nil
}

// Discard removes a claimed plan once its outcome is known.
//
// It is called whether the change succeeded or failed. A write that failed
// mid-flight may still have reached Zabbix, so the plan is not put back for
// another attempt: the caller is told to look at the current state and plan
// again.
func (s *Store) Discard(id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path + claimedSuffix); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
