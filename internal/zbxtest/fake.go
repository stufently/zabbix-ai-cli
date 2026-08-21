// Package zbxtest provides a fake Zabbix API for tests.
//
// It exists so the test suite never needs a real server: every behaviour worth
// asserting — an empty result, a rejected token, a malformed history type — is
// easier to provoke here than against a live installation.
package zbxtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request is one call the fake received.
type Request struct {
	Method string
	Params map[string]any
	Auth   string
}

// Handler answers one API method. Returning an error makes the fake reply with
// a Zabbix-style error object.
type Handler func(params map[string]any) (any, error)

// APIError is a Zabbix error a handler can return.
type APIError struct {
	Code    int
	Message string
	Data    string
}

func (e *APIError) Error() string { return e.Message }

// Server is a fake Zabbix endpoint.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	handlers map[string]Handler
	calls    []Request
}

// New starts a fake Zabbix server that reports the given API version.
func New(t *testing.T, version string) *Server {
	t.Helper()
	s := &Server{handlers: map[string]Handler{}}
	s.Handle("apiinfo.version", func(map[string]any) (any, error) { return version, nil })
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Handle registers an answer for a method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Reply registers a fixed answer for a method.
//
// The answer is passed through the same "filter" semantics Zabbix applies:
// exact equality on each named field. Without that a fake would answer an
// exact-name lookup with every row it holds, and a caller that relies on the
// filter would look correct here and be wrong against a real server.
func (s *Server) Reply(method string, result any) {
	s.Handle(method, func(params map[string]any) (any, error) {
		return applyFilter(result, params), nil
	})
}

func applyFilter(result any, params map[string]any) any {
	filter, ok := params["filter"].(map[string]any)
	if !ok || len(filter) == 0 {
		return result
	}
	rows, ok := result.([]any)
	if !ok {
		return result
	}
	kept := make([]any, 0, len(rows))
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			continue
		}
		match := true
		for k, want := range filter {
			if !filterMatches(fields[k], want) {
				match = false
				break
			}
		}
		if match {
			kept = append(kept, row)
		}
	}
	return kept
}

func filterMatches(got, want any) bool {
	if list, ok := want.([]any); ok {
		for _, w := range list {
			if got == w {
				return true
			}
		}
		return false
	}
	return got == want
}

// Fail registers a Zabbix error for a method.
func (s *Server) Fail(method string, code int, message, data string) {
	s.Handle(method, func(map[string]any) (any, error) {
		return nil, &APIError{Code: code, Message: message, Data: data}
	})
}

// Calls returns every request the fake received, in order.
func (s *Server) Calls() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.calls))
	copy(out, s.calls)
	return out
}

// CallsTo returns the requests for one method.
func (s *Server) CallsTo(method string) []Request {
	var out []Request
	for _, c := range s.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Reset forgets the recorded calls.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
		ID     int            `json:"id"`
	}
	_ = json.Unmarshal(body, &req)

	s.mu.Lock()
	s.calls = append(s.calls, Request{Method: req.Method, Params: req.Params, Auth: r.Header.Get("Authorization")})
	h, ok := s.handlers[req.Method]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		writeError(w, req.ID, &APIError{Code: -32602, Message: "Invalid params.",
			Data: `Method "` + req.Method + `" not registered in the fake server.`})
		return
	}
	result, err := h(req.Params)
	if err != nil {
		apiErr, isAPI := err.(*APIError)
		if !isAPI {
			apiErr = &APIError{Code: -32500, Message: err.Error()}
		}
		writeError(w, req.ID, apiErr)
		return
	}
	encoded, mErr := json.Marshal(result)
	if mErr != nil {
		writeError(w, req.ID, &APIError{Code: -32603, Message: mErr.Error()})
		return
	}
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":` + string(encoded) + `,"id":1}`))
}

func writeError(w http.ResponseWriter, id int, e *APIError) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": e.Code, "message": e.Message, "data": e.Data},
		"id":      id,
	})
	_, _ = w.Write(payload)
}

// Fixtures for the objects tests need most often.

// Host returns a host.get row.
func Host(id, name string, extra map[string]any) map[string]any {
	h := map[string]any{
		"hostid": id, "host": name, "name": name,
		"status": "0", "active_available": "1",
		"maintenance_status": "0", "maintenanceid": "0", "description": "",
		"interfaces": []any{map[string]any{
			"interfaceid": "1", "ip": "10.0.0.1", "dns": "", "port": "10050",
			"type": "1", "main": "1", "useip": "1", "available": "1", "error": "",
		}},
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// Problem returns a problem.get row.
func Problem(eventID, triggerID, name, severity string, extra map[string]any) map[string]any {
	p := map[string]any{
		"eventid": eventID, "objectid": triggerID, "clock": "1787000000",
		"name": name, "severity": severity, "acknowledged": "0",
		"suppressed": "0", "cause_eventid": "0", "opdata": "",
		"tags": []any{}, "suppression_data": []any{},
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// Item returns an item.get row.
func Item(id, hostID, name, key, valueType string, extra map[string]any) map[string]any {
	i := map[string]any{
		"itemid": id, "hostid": hostID, "name": name, "key_": key,
		"value_type": valueType, "units": "", "delay": "1m",
		"status": "0", "state": "0", "error": "", "type": "0",
	}
	for k, v := range extra {
		i[k] = v
	}
	return i
}
