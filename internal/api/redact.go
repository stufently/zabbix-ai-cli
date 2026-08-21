package api

import (
	"encoding/json"
	"strings"
)

// Redacted replaces the value of every sensitive field.
const Redacted = "[REDACTED]"

var sensitiveKeys = map[string]bool{
	"password":    true,
	"token":       true,
	"auth":        true,
	"sessionid":   true,
	"current_pas": true,
	"secret":      true,
	"private_key": true,
	"ssh_key":     true,
	"tls_psk":     true,
}

// redactBody prepares a request body for debug logging. Values of sensitive
// keys are replaced at any depth. Anything that fails to parse is reported as
// an opaque marker rather than logged verbatim, so a malformed body can never
// leak a credential.
func redactBody(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "[unparsable body omitted]"
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return "[unserialisable body omitted]"
	}
	const max = 2000
	s := string(out)
	if len(s) > max {
		return s[:max] + "...[truncated]"
	}
	return s
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if isSensitive(k) {
				t[k] = Redacted
				continue
			}
			redactValue(child)
		}
	case []any:
		for _, child := range t {
			redactValue(child)
		}
	}
}

func isSensitive(key string) bool {
	k := strings.ToLower(key)
	if sensitiveKeys[k] {
		return true
	}
	return strings.Contains(k, "password") || strings.Contains(k, "secret") ||
		strings.HasSuffix(k, "_token") || k == "apitoken"
}

// Redact returns a deep copy of v with the value of every sensitive field
// replaced. It is used for the audit log, which records what was done without
// recording a credential that was passed along the way.
func Redact(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return Redacted
	}
	var copied any
	if err := json.Unmarshal(data, &copied); err != nil {
		return Redacted
	}
	redactValue(copied)
	return copied
}
