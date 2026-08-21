// Package output defines the JSON envelope that agents consume and the
// human-facing table renderer.
//
// The envelope is a stable contract: fields are added, never repurposed.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format selects a rendering.
type Format string

const (
	FormatJSON  Format = "json"
	FormatTable Format = "table"
)

// Meta describes the shape of a result so an agent can tell a complete answer
// from a bounded one.
type Meta struct {
	Returned        int    `json:"returned"`
	Total           *int   `json:"total,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Truncated       bool   `json:"truncated"`
	TruncatedReason string `json:"truncated_reason,omitempty"`
	NextCursor      string `json:"next_cursor,omitempty"`
	Partial         bool   `json:"partial"`
	ZabbixVersion   string `json:"zabbix_version,omitempty"`
	Profile         string `json:"profile,omitempty"`
	ElapsedMS       int64  `json:"elapsed_ms,omitempty"`
}

// Truncation reasons.
const (
	ReasonRowLimit  = "row_limit"
	ReasonByteLimit = "byte_limit"
)

// Result is what an operation returns.
type Result struct {
	Data     any      `json:"-"`
	Warnings []string `json:"-"`
	Meta     Meta     `json:"-"`
	// Table, when set, renders the human format. Without it, table output
	// falls back to indented JSON rather than inventing a layout.
	Table *Table `json:"-"`
}

// Table is a rendered human view of a result.
type Table struct {
	Headers []string
	Rows    [][]string
	// Notes are printed under the table; used for truncation hints.
	Notes []string
}

// Warn appends a warning, ignoring duplicates.
func (r *Result) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, w := range r.Warnings {
		if w == msg {
			return
		}
	}
	r.Warnings = append(r.Warnings, msg)
}

// Envelope is the serialised form of a success.
type Envelope struct {
	OK       bool     `json:"ok"`
	Data     any      `json:"data"`
	Warnings []string `json:"warnings"`
	Meta     Meta     `json:"meta"`
}

// ErrorEnvelope is the serialised form of a failure.
type ErrorEnvelope struct {
	OK    bool              `json:"ok"`
	Error ErrorEnvelopeBody `json:"error"`
	Meta  *Meta             `json:"meta,omitempty"`
}

// ErrorEnvelopeBody carries the agent-facing error contract.
type ErrorEnvelopeBody struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	Suggestion string `json:"suggestion,omitempty"`
}

// WriteJSON renders a successful result as the JSON envelope.
func WriteJSON(w io.Writer, r *Result) error {
	env := Envelope{OK: true, Data: r.Data, Warnings: r.Warnings, Meta: r.Meta}
	if env.Warnings == nil {
		env.Warnings = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// WriteErrorJSON renders a failure as the JSON envelope.
func WriteErrorJSON(w io.Writer, body ErrorEnvelopeBody) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(ErrorEnvelope{OK: false, Error: body})
}

// WriteTable renders the human format, falling back to indented JSON when the
// operation supplied no table.
func WriteTable(w io.Writer, r *Result) error {
	for _, warn := range r.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	if r.Table == nil {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r.Data)
	}
	renderTable(w, r.Table)
	if r.Meta.Truncated {
		total := ""
		if r.Meta.Total != nil {
			total = fmt.Sprintf(" of %d", *r.Meta.Total)
		}
		fmt.Fprintf(w, "\nshowing %d%s results; raise --limit to see more\n", r.Meta.Returned, total)
	}
	for _, n := range r.Table.Notes {
		fmt.Fprintf(w, "%s\n", n)
	}
	return nil
}

func renderTable(w io.Writer, t *Table) {
	if len(t.Rows) == 0 {
		fmt.Fprintln(w, "no results")
		return
	}
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	writeRow(w, t.Headers, widths)
	for _, row := range t.Rows {
		writeRow(w, row, widths)
	}
}

func writeRow(w io.Writer, cells []string, widths []int) {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		if i < len(cells)-1 && i < len(widths) {
			for n := utf8.RuneCountInString(cell); n < widths[i]; n++ {
				b.WriteByte(' ')
			}
		}
	}
	fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
}

// MaxFieldLength bounds a single untrusted string field.
const MaxFieldLength = 512

// Sanitise prepares a string that originated in Zabbix for inclusion in a
// result. Control characters are removed and the value is length-bounded.
//
// Values reaching an agent are data, never instructions; stripping control
// characters keeps them from forging terminal or protocol structure, and the
// length bound keeps one pathological item from flooding a context window.
func Sanitise(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		case r == utf8.RuneError:
			continue
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > MaxFieldLength {
		runes := []rune(out)
		return string(runes[:MaxFieldLength]) + "..."
	}
	return out
}

// Bound trims items to limit, reporting whether anything was dropped. Callers
// fetch limit+1 rows so that truncation is detected without a second count
// query.
func Bound[T any](items []T, limit int) (kept []T, truncated bool) {
	// A negative limit is a caller mistake, not a request for everything: it
	// used to fall into the "no limit" branch and hand back the whole result
	// set. Zero still means "the operation's own default applies".
	if limit < 0 {
		limit = 0
	}
	if limit == 0 || len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}

// SortedKeys returns the keys of m in a stable order, so that output does not
// vary between runs.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
