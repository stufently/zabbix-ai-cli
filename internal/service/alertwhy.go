package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stufently/zabbix-ai-cli/internal/errs"
	"github.com/stufently/zabbix-ai-cli/internal/output"
)

// Alert delivery statuses as reported by alert.get.
const (
	alertStatusNotSent = "0"
	alertStatusSent    = "1"
	alertStatusFailed  = "2"
	alertStatusNew     = "3"
)

// EventSummary identifies the event an explanation is about.
type EventSummary struct {
	EventID      string        `json:"eventid"`
	Name         string        `json:"name"`
	Severity     string        `json:"severity"`
	SeverityCode int           `json:"severity_code"`
	Started      string        `json:"started"`
	Age          string        `json:"age,omitempty"`
	Resolved     bool          `json:"resolved"`
	Acknowledged bool          `json:"acknowledged"`
	Hosts        []ProblemHost `json:"hosts,omitempty"`
	TriggerID    string        `json:"triggerid,omitempty"`
}

// DeliveryAttempt is one notification Zabbix tried to send.
type DeliveryAttempt struct {
	AlertID   string `json:"alertid"`
	MediaType string `json:"media_type"`
	SendTo    string `json:"send_to,omitempty"`
	Status    string `json:"status"`
	Retries   int    `json:"retries"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Kind      string `json:"kind"`
}

// ActionCheck reports one trigger action and whether it could have fired.
type ActionCheck struct {
	ActionID   string   `json:"actionid"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Operations int      `json:"operations"`
	Recipients []string `json:"recipients,omitempty"`
}

// MediaTypeCheck reports the state of a media type.
type MediaTypeCheck struct {
	ID      string `json:"mediatypeid"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// RecipientCheck reports whether one user could have received the event.
type RecipientCheck struct {
	User             string `json:"user"`
	MediaType        string `json:"media_type"`
	SendTo           string `json:"send_to,omitempty"`
	MediaEnabled     bool   `json:"media_enabled"`
	SeverityAccepted bool   `json:"severity_accepted"`
	Period           string `json:"period,omitempty"`
}

// AlertExplanation gathers every fact that bears on whether a notification was
// sent for an event.
//
// It states facts and leaves the conclusion to the reader: answering "why was
// there no alert" previously took three ad-hoc scripts, and the value is in
// collecting the chain reliably, not in guessing which link broke.
type AlertExplanation struct {
	Event        EventSummary      `json:"event"`
	Suppressed   bool              `json:"suppressed"`
	SuppressedBy []Suppression     `json:"suppressed_by,omitempty"`
	Attempts     []DeliveryAttempt `json:"delivery_attempts"`
	Actions      []ActionCheck     `json:"actions,omitempty"`
	MediaTypes   []MediaTypeCheck  `json:"media_types,omitempty"`
	Recipients   []RecipientCheck  `json:"recipients,omitempty"`
	Findings     []string          `json:"findings"`
	Partial      bool              `json:"-"`
	Warnings     []string          `json:"-"`
}

type wireEvent struct {
	EventID      string `json:"eventid"`
	Clock        string `json:"clock"`
	Name         string `json:"name"`
	Severity     string `json:"severity"`
	Acknowledged string `json:"acknowledged"`
	Value        string `json:"value"`
	ObjectID     string `json:"objectid"`
	RClock       string `json:"r_eventid"`
	Hosts        []struct {
		HostID string `json:"hostid"`
		Name   string `json:"name"`
	} `json:"hosts"`
	SuppressionData []struct {
		MaintenanceID string `json:"maintenanceid"`
		SuppressUntil string `json:"suppress_until"`
		UserID        string `json:"userid"`
	} `json:"suppression_data"`
}

// ExplainAlert assembles the notification chain for an event.
func (s *Service) ExplainAlert(ctx context.Context, eventID string) (*AlertExplanation, error) {
	ev, err := s.getEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	exp := &AlertExplanation{Event: ev.summary()}
	for _, sd := range ev.SuppressionData {
		sup := Suppression{MaintenanceID: sd.MaintenanceID, ByUser: sd.UserID != "" && sd.UserID != "0"}
		if until := unixToTime(sd.SuppressUntil); until.IsZero() {
			sup.Indefinite = true
		} else {
			sup.Until = rfc3339(until)
		}
		exp.SuppressedBy = append(exp.SuppressedBy, sup)
	}
	exp.Suppressed = len(exp.SuppressedBy) > 0
	if exp.Suppressed {
		if names, err := s.maintenanceNames(ctx, maintenanceIDs(exp.SuppressedBy)); err == nil {
			for i, sup := range exp.SuppressedBy {
				if n, ok := names[sup.MaintenanceID]; ok {
					exp.SuppressedBy[i].MaintenanceName = n
				}
			}
		}
		exp.Findings = append(exp.Findings, "the event is suppressed: "+SuppressionSummary(exp.SuppressedBy))
	}

	attempts, err := s.deliveryAttempts(ctx, eventID)
	if err != nil {
		exp.Partial = true
		exp.Warnings = append(exp.Warnings, "delivery attempts could not be read: "+err.Error())
	}
	exp.Attempts = attempts

	sent, failed := 0, 0
	for _, a := range attempts {
		switch a.Status {
		case "sent":
			sent++
		case "failed":
			failed++
		}
	}
	switch {
	case sent > 0 && failed == 0:
		exp.Findings = append(exp.Findings, fmt.Sprintf("Zabbix sent %d notification(s) for this event", sent))
	case failed > 0:
		exp.Findings = append(exp.Findings, fmt.Sprintf("%d of %d delivery attempts failed", failed, len(attempts)))
	case len(attempts) == 0:
		exp.Findings = append(exp.Findings, "Zabbix recorded no delivery attempt for this event")
	}

	// The configuration chain only needs inspecting when nothing was sent.
	if len(attempts) == 0 {
		if err := s.inspectNotificationConfig(ctx, exp); err != nil {
			exp.Partial = true
			exp.Warnings = append(exp.Warnings, err.Error())
		}
	}
	if len(exp.Findings) == 0 {
		exp.Findings = append(exp.Findings, "no obstacle to notification was found in the configuration")
	}
	return exp, nil
}

func maintenanceIDs(sups []Suppression) []string {
	ids := make([]string, 0, len(sups))
	for _, s := range sups {
		if s.MaintenanceID != "" && s.MaintenanceID != "0" {
			ids = append(ids, s.MaintenanceID)
		}
	}
	return ids
}

func (s *Service) getEvent(ctx context.Context, eventID string) (*wireEvent, error) {
	params := map[string]any{
		"output":                []string{"eventid", "clock", "name", "severity", "acknowledged", "value", "objectid", "r_eventid"},
		"eventids":              []string{eventID},
		"selectHosts":           []string{"hostid", "name"},
		"selectSuppressionData": "extend",
	}
	var events []wireEvent
	if err := s.client.CallIdempotent(ctx, "event.get", params, &events); err != nil {
		return nil, errs.FromAPI(err)
	}
	if len(events) == 0 {
		return nil, errs.NotFound("event %s does not exist", eventID).
			WithSuggestion("event IDs come from 'zabbix-ai-cli problems list'; a pasted alert can be resolved with 'zabbix-ai-cli resolve'")
	}
	return &events[0], nil
}

func (e *wireEvent) summary() EventSummary {
	started := unixToTime(e.Clock)
	sum := EventSummary{
		EventID:      e.EventID,
		Name:         output.Sanitise(e.Name),
		Severity:     SeverityName(e.Severity),
		SeverityCode: atoi(e.Severity),
		Started:      rfc3339(started),
		Acknowledged: e.Acknowledged == "1",
		Resolved:     e.RClock != "" && e.RClock != "0",
		TriggerID:    e.ObjectID,
	}
	if !started.IsZero() {
		sum.Age = HumanDuration(time.Since(started))
	}
	for _, h := range e.Hosts {
		sum.Hosts = append(sum.Hosts, ProblemHost{ID: h.HostID, Name: output.Sanitise(h.Name)})
	}
	return sum
}

func (s *Service) deliveryAttempts(ctx context.Context, eventID string) ([]DeliveryAttempt, error) {
	var wire []struct {
		AlertID     string `json:"alertid"`
		MediaTypeID string `json:"mediatypeid"`
		SendTo      string `json:"sendto"`
		Status      string `json:"status"`
		Retries     string `json:"retries"`
		Error       string `json:"error"`
		Clock       string `json:"clock"`
		AlertType   string `json:"alerttype"`
	}
	params := map[string]any{
		"output":   []string{"alertid", "mediatypeid", "sendto", "status", "retries", "error", "clock", "alerttype"},
		"eventids": []string{eventID},
		"limit":    100,
	}
	if err := s.client.CallIdempotent(ctx, "alert.get", params, &wire); err != nil {
		return nil, errs.FromAPI(err)
	}
	names, _ := s.mediaTypeNames(ctx)
	attempts := make([]DeliveryAttempt, 0, len(wire))
	for _, w := range wire {
		a := DeliveryAttempt{
			AlertID:   w.AlertID,
			MediaType: names[w.MediaTypeID],
			SendTo:    output.Sanitise(w.SendTo),
			Status:    alertStatusName(w.Status),
			Retries:   atoi(w.Retries),
			Error:     output.Sanitise(w.Error),
			Timestamp: rfc3339(unixToTime(w.Clock)),
			Kind:      alertKind(w.AlertType),
		}
		if a.MediaType == "" {
			a.MediaType = "media type " + w.MediaTypeID
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}

func alertStatusName(v string) string {
	switch v {
	case alertStatusNotSent:
		return "not sent"
	case alertStatusSent:
		return "sent"
	case alertStatusFailed:
		return "failed"
	case alertStatusNew:
		return "queued"
	default:
		return "unknown"
	}
}

func alertKind(v string) string {
	if v == "1" {
		return "remote command"
	}
	return "message"
}

func (s *Service) mediaTypeNames(ctx context.Context) (map[string]string, error) {
	types, err := s.mediaTypes(ctx)
	if err != nil {
		return map[string]string{}, err
	}
	names := make(map[string]string, len(types))
	for _, t := range types {
		names[t.ID] = t.Name
	}
	return names, nil
}

func (s *Service) mediaTypes(ctx context.Context) ([]MediaTypeCheck, error) {
	var wire []struct {
		MediaTypeID string `json:"mediatypeid"`
		Name        string `json:"name"`
		Status      string `json:"status"`
	}
	params := map[string]any{"output": []string{"mediatypeid", "name", "status"}}
	if err := s.client.CallIdempotent(ctx, "mediatype.get", params, &wire); err != nil {
		return nil, errs.FromAPI(err)
	}
	out := make([]MediaTypeCheck, 0, len(wire))
	for _, w := range wire {
		out = append(out, MediaTypeCheck{
			ID: w.MediaTypeID, Name: output.Sanitise(w.Name), Enabled: w.Status == "0",
		})
	}
	return out, nil
}

type wireAction struct {
	ActionID    string `json:"actionid"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	EventSource string `json:"eventsource"`
	Operations  []struct {
		OperationType string `json:"operationtype"`
		OpMessage     struct {
			MediaTypeID string `json:"mediatypeid"`
		} `json:"opmessage"`
		OpMessageGrp []struct {
			UsrGrpID string `json:"usrgrpid"`
		} `json:"opmessage_grp"`
		OpMessageUsr []struct {
			UserID string `json:"userid"`
		} `json:"opmessage_usr"`
	} `json:"operations"`
}

// inspectNotificationConfig walks the configuration that decides whether a
// trigger event produces a message at all.
func (s *Service) inspectNotificationConfig(ctx context.Context, exp *AlertExplanation) error {
	var actions []wireAction
	params := map[string]any{
		"output":           []string{"actionid", "name", "status", "eventsource"},
		"filter":           map[string]any{"eventsource": 0},
		"selectOperations": "extend",
		"limit":            50,
	}
	if err := s.client.CallIdempotent(ctx, "action.get", params, &actions); err != nil {
		return errs.FromAPI(err)
	}

	enabled := 0
	var groupIDs, userIDs []string
	for _, a := range actions {
		check := ActionCheck{
			ActionID:   a.ActionID,
			Name:       output.Sanitise(a.Name),
			Enabled:    a.Status == "0",
			Operations: len(a.Operations),
		}
		if check.Enabled {
			enabled++
			for _, op := range a.Operations {
				for _, g := range op.OpMessageGrp {
					groupIDs = appendUnique(groupIDs, g.UsrGrpID)
				}
				for _, u := range op.OpMessageUsr {
					userIDs = appendUnique(userIDs, u.UserID)
				}
			}
		}
		exp.Actions = append(exp.Actions, check)
	}
	switch {
	case len(actions) == 0:
		exp.Findings = append(exp.Findings, "no trigger action exists, so no notification can ever be sent")
	case enabled == 0:
		exp.Findings = append(exp.Findings, "every trigger action is disabled")
	}

	types, err := s.mediaTypes(ctx)
	if err != nil {
		return err
	}
	exp.MediaTypes = types
	enabledTypes := 0
	var disabled []string
	for _, t := range types {
		if t.Enabled {
			enabledTypes++
		} else {
			disabled = append(disabled, t.Name)
		}
	}
	if enabledTypes == 0 && len(types) > 0 {
		exp.Findings = append(exp.Findings, "every media type is disabled")
	} else if len(disabled) > 0 {
		exp.Findings = append(exp.Findings, "disabled media types: "+strings.Join(disabled, ", "))
	}

	if enabled > 0 {
		if err := s.checkRecipients(ctx, exp, groupIDs, userIDs, types); err != nil {
			return err
		}
	}
	return nil
}

// checkRecipients tests each candidate recipient's media against the event.
//
// A user media carries a severity bitmask and an active period; either can
// drop a notification without any error being recorded anywhere.
func (s *Service) checkRecipients(ctx context.Context, exp *AlertExplanation, groupIDs, userIDs []string, types []MediaTypeCheck) error {
	if len(groupIDs) == 0 && len(userIDs) == 0 {
		exp.Findings = append(exp.Findings, "no enabled action names any user or user group to notify")
		return nil
	}
	params := map[string]any{
		"output":       []string{"userid", "username", "name"},
		"selectMedias": "extend",
		"limit":        100,
	}
	if len(groupIDs) > 0 {
		params["usrgrpids"] = groupIDs
	}
	if len(userIDs) > 0 {
		params["userids"] = userIDs
	}
	var users []struct {
		UserID   string `json:"userid"`
		Username string `json:"username"`
		Medias   []struct {
			MediaTypeID string `json:"mediatypeid"`
			SendTo      any    `json:"sendto"`
			Active      string `json:"active"`
			Severity    string `json:"severity"`
			Period      string `json:"period"`
		} `json:"medias"`
	}
	if err := s.client.CallIdempotent(ctx, "user.get", params, &users); err != nil {
		return errs.FromAPI(err)
	}

	typeNames := map[string]string{}
	for _, t := range types {
		typeNames[t.ID] = t.Name
	}
	severityBit := 1 << exp.Event.SeverityCode
	reachable := 0
	withMedia := 0
	for _, u := range users {
		for _, m := range u.Medias {
			withMedia++
			accepted := atoi(m.Severity)&severityBit != 0
			active := m.Active == "0"
			name := typeNames[m.MediaTypeID]
			if name == "" {
				name = "media type " + m.MediaTypeID
			}
			exp.Recipients = append(exp.Recipients, RecipientCheck{
				User:             output.Sanitise(u.Username),
				MediaType:        name,
				SendTo:           output.Sanitise(sendToString(m.SendTo)),
				MediaEnabled:     active,
				SeverityAccepted: accepted,
				Period:           output.Sanitise(m.Period),
			})
			if accepted && active {
				reachable++
			}
		}
	}
	switch {
	case withMedia == 0:
		exp.Findings = append(exp.Findings, "no notified user has any media configured")
	case reachable == 0:
		exp.Findings = append(exp.Findings,
			fmt.Sprintf("no user media accepts severity %q; every entry is disabled or filters this severity out", exp.Event.Severity))
	}
	return nil
}

// sendToString normalises the sendto field, which Zabbix returns as a string
// for most media types and as an array for email.
func sendToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

func appendUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
