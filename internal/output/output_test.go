package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeShapeIsStable(t *testing.T) {
	var buf bytes.Buffer
	total := 381
	r := &Result{
		Data: map[string]any{"host": "web01"},
		Meta: Meta{Returned: 50, Total: &total, Limit: 50, Truncated: true, TruncatedReason: ReasonRowLimit},
	}
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env["ok"] != true {
		t.Errorf("ok = %v", env["ok"])
	}
	if _, ok := env["warnings"].([]any); !ok {
		t.Errorf("warnings must always be an array, got %T", env["warnings"])
	}
	meta, ok := env["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta missing")
	}
	for _, key := range []string{"returned", "total", "truncated", "truncated_reason", "partial"} {
		if _, ok := meta[key]; !ok {
			t.Errorf("meta.%s missing from the envelope", key)
		}
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	err := WriteErrorJSON(&buf, ErrorEnvelopeBody{
		Code: "HOST_NOT_FOUND", Message: "host 'x' was not found", Suggestion: "try host list",
	})
	if err != nil {
		t.Fatalf("WriteErrorJSON: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env["ok"] != false {
		t.Errorf("ok = %v, want false", env["ok"])
	}
	body := env["error"].(map[string]any)
	if body["code"] != "HOST_NOT_FOUND" || body["retryable"] != false {
		t.Errorf("error body = %v", body)
	}
}

func TestSanitiseStripsControlCharacters(t *testing.T) {
	in := "web01\x1b[31m\x00 down\nnow"
	got := Sanitise(in)
	if strings.ContainsAny(got, "\x1b\x00\n") {
		t.Errorf("Sanitise left control characters: %q", got)
	}
	if !strings.Contains(got, "web01") || !strings.Contains(got, "down") {
		t.Errorf("Sanitise removed real content: %q", got)
	}
}

func TestSanitiseBoundsLength(t *testing.T) {
	got := Sanitise(strings.Repeat("a", MaxFieldLength*3))
	if len([]rune(got)) > MaxFieldLength+3 {
		t.Errorf("Sanitise returned %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncation was not marked: %q", got[len(got)-10:])
	}
}

func TestBoundDetectsTruncation(t *testing.T) {
	items := []int{1, 2, 3, 4}
	kept, truncated := Bound(items, 3)
	if !truncated || len(kept) != 3 {
		t.Fatalf("Bound = %v, %v", kept, truncated)
	}
	kept, truncated = Bound(items, 4)
	if truncated || len(kept) != 4 {
		t.Fatalf("Bound = %v, %v", kept, truncated)
	}
	kept, truncated = Bound(items, 0)
	if truncated || len(kept) != 4 {
		t.Fatalf("Bound with no limit = %v, %v", kept, truncated)
	}
}

func TestTableFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTable(&buf, &Result{Data: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	if !strings.Contains(buf.String(), `"a"`) {
		t.Errorf("fallback did not render JSON: %s", buf.String())
	}
}

func TestTableRendersRows(t *testing.T) {
	var buf bytes.Buffer
	r := &Result{Table: &Table{
		Headers: []string{"HOST", "STATE"},
		Rows:    [][]string{{"web01", "up"}, {"database-primary", "down"}},
	}}
	if err := WriteTable(&buf, r); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "HOST") || !strings.Contains(out, "database-primary") {
		t.Errorf("table output = %q", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Errorf("want header plus two rows, got %d lines", len(lines))
	}
}
