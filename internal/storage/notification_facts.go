package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/sandbox"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The alarm fact map, declared once (#618, #1319), exactly as the label data
// map is (#729, label_keys.go): each key carries its name, the summary the docs
// render and the accessor that produces the value, so the map a notification
// template reads, the docs table that teaches it and the expression site an
// action rule's predicate reads are all built from this one list.
//
// This declaration IS the sandbox's data half for a notification. A key is the
// only way to widen what a template can see, so a secret, a credential, a
// target's URL or a handle to another entity is not filtered out downstream,
// it is never added. Every value is a string.

// Transition names the alarm edge a notification is about.
const (
	TransitionRaised  = "raised"
	TransitionCleared = "cleared"
)

// alarmFactRow is the one alarm transition the fact map is read from: the
// alarm, its component's classification and placement, and the install's
// console base.
type alarmFactRow struct {
	alarmID, componentID, component, componentLabel string
	typeName, place                                 string
	severity, severityLabel, message                string
	raisedAt                                        time.Time
	clearedAt                                       *time.Time
	rule                                            string
	transition                                      string
	consoleURL                                      string
}

func factTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

var alarmFactKeys = []labelKey[alarmFactRow]{
	{Name: "Component", Summary: "The component's name.", value: func(f alarmFactRow) string { return f.component }},
	{Name: "ComponentLabel", Summary: "How the component reads: its label, else its name.", value: func(f alarmFactRow) string {
		if f.componentLabel != "" {
			return f.componentLabel
		}
		return f.component
	}},
	{Name: "ComponentType", Summary: "The label of the component type its product classifies it as; empty for a component with no product.", value: func(f alarmFactRow) string { return f.typeName }},
	{Name: "Place", Summary: "How the location the component sits at reads (its label, else its name); empty when it sits at none of its own.", value: func(f alarmFactRow) string { return f.place }},
	{Name: "Severity", Summary: "The severity level the alarm was raised at, by name.", value: func(f alarmFactRow) string { return f.severity }},
	{Name: "SeverityLabel", Summary: "That level's label, else its name.", value: func(f alarmFactRow) string {
		if f.severityLabel != "" {
			return f.severityLabel
		}
		return f.severity
	}},
	{Name: "Message", Summary: "The alarm's message: what its rule says, or what the operator who raised it wrote.", value: func(f alarmFactRow) string { return f.message }},
	{Name: "Transition", Summary: "The edge this message is about: raised or cleared.", value: func(f alarmFactRow) string { return f.transition }},
	{Name: "RaisedAt", Summary: "When the alarm was raised, RFC 3339 in UTC.", value: func(f alarmFactRow) string { return factTime(&f.raisedAt) }},
	{Name: "ClearedAt", Summary: "When it was cleared, RFC 3339 in UTC; empty while it is raised.", value: func(f alarmFactRow) string { return factTime(f.clearedAt) }},
	{Name: "Rule", Summary: "The event rule that raised it, by name; empty for an alarm an operator raised.", value: func(f alarmFactRow) string { return f.rule }},
	{Name: "Link", Summary: "The component's page in the console, built on the notification.console_url setting; empty while that is unset.", value: func(f alarmFactRow) string {
		if f.consoleURL == "" {
			return ""
		}
		return strings.TrimRight(f.consoleURL, "/") + "/components/" + f.componentID
	}},
}

// AlarmFactKeys are the keys a notification template and an action rule's
// predicate may read, in documented order. Exported for the API's field
// descriptions and the expression site, which a test holds to this list.
func AlarmFactKeys() []string { return keyNames(alarmFactKeys) }

// alarmFactData builds the closed map from the declaration, each value cut to
// the sandbox's MaxFact, so the facts a preview shows and an envelope carries
// are the ones the template read (an alarm's message has no length cap of
// its own).
func alarmFactData(f alarmFactRow) sandbox.Data {
	d := make(sandbox.Data, len(alarmFactKeys))
	for _, k := range alarmFactKeys {
		d[k.Name] = k.value(f)
	}
	return sandbox.BoundFacts(d)
}

// alarmFacts reads one alarm on a component (both already resolved by the
// caller) and builds its fact map for a transition: an empty transition reads
// the alarm's own state. An alarm not on that component is ErrAlarmNotFound.
func (p *PG) alarmFacts(ctx context.Context, q querier, componentID, alarmID, transition string) (sandbox.Data, string, error) {
	if !isUUID(alarmID) || !isUUID(componentID) {
		return nil, "", ErrAlarmNotFound
	}
	f, productID, err := readAlarmFactRow(ctx, q, componentID, alarmID)
	if err != nil {
		return nil, "", err
	}
	switch transition {
	case "":
		f.transition = TransitionRaised
		if f.clearedAt != nil {
			f.transition = TransitionCleared
		}
	case TransitionRaised, TransitionCleared:
		f.transition = transition
	default:
		return nil, "", fmt.Errorf("%w: transition must be raised or cleared", ErrNotificationTypeInvalid)
	}
	if productID != nil {
		in, err := componentLabelChain(ctx, q, *productID)
		if err != nil && !errors.Is(err, ErrProductNotFound) {
			return nil, "", err
		}
		f.typeName = in.typeName
	}
	places, err := componentPlacements(ctx, q, []string{componentID})
	if err != nil {
		return nil, "", err
	}
	f.place = places[componentID].locationLabel
	s, err := p.platformSettings(ctx, q)
	if err != nil {
		return nil, "", err
	}
	f.consoleURL = s.Notification.ConsoleURL
	return alarmFactData(f), f.transition, nil
}

func readAlarmFactRow(ctx context.Context, q querier, componentID, alarmID string) (alarmFactRow, *string, error) {
	var f alarmFactRow
	var productID *string
	err := q.QueryRow(ctx, `
		select a.id::text, c.id::text, c.name, coalesce(c.label, ''), c.product_id::text,
			a.severity, coalesce(sl.label, ''), a.message, a.raised_at, a.cleared_at, coalesce(r.name, '')
		from alarm a
		join component c on c.id = a.component_id
		left join severity_level sl on sl.name = a.severity
		left join event_rule r on r.id = a.raised_by_rule_id
		where a.id = $1::uuid and a.component_id = $2::uuid`, alarmID, componentID).
		Scan(&f.alarmID, &f.componentID, &f.component, &f.componentLabel, &productID,
			&f.severity, &f.severityLabel, &f.message, &f.raisedAt, &f.clearedAt, &f.rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nil, ErrAlarmNotFound
	}
	if err != nil {
		return f, nil, fmt.Errorf("storage: read alarm %s facts: %w", alarmID, err)
	}
	return f, productID, nil
}

// platformSettings resolves the platform settings in force, in the caller's
// querier (see labelEngine for why the querier is the caller's).
func (p *PG) platformSettings(ctx context.Context, q querier) (settings.Settings, error) {
	doc, locks, err := p.settingLevel(ctx, q, "platform")
	if err != nil {
		return settings.Settings{}, fmt.Errorf("storage: resolve platform settings: %w", err)
	}
	s, err := settings.Typed(p.settings.ResolveOverride(doc, locks).Values)
	if err != nil {
		return settings.Settings{}, fmt.Errorf("storage: resolve platform settings: %w", err)
	}
	return s, nil
}

// notificationEngine is the template engine a notification renders through,
// on the same acronym dictionary a label's `title` uses.
func (p *PG) notificationEngine(ctx context.Context, q querier) (*sandbox.Engine, error) {
	s, err := p.platformSettings(ctx, q)
	if err != nil {
		return nil, err
	}
	return sandbox.New(s.Label.Acronyms), nil
}

// The rendered sizes a notification carries. The subject is one line, so its
// whitespace collapses as a label's does; the text keeps its lines and is
// trimmed at the ends. Past either cap the render fails rather than cutting a
// message in half.
const (
	NotificationSubjectMax = 200
	NotificationTextMax    = 4000
)

// ParseNotificationTemplate compiles one notification template through the
// sandbox's grammar: the edit-time check, so a template it refuses is never
// stored.
func ParseNotificationTemplate(src string) (*sandbox.Template, error) {
	return sandbox.Basic.Parse(src)
}

// renderNotification renders a type's two templates over one fact map.
func renderNotification(eng *sandbox.Engine, subject, text string, facts sandbox.Data) (string, string, error) {
	st, err := eng.Parse(subject)
	if err != nil {
		return "", "", fmt.Errorf("subject: %w", err)
	}
	tt, err := eng.Parse(text)
	if err != nil {
		return "", "", fmt.Errorf("text: %w", err)
	}
	sub, err := st.Execute(facts, NotificationSubjectMax)
	if err != nil {
		return "", "", fmt.Errorf("subject: %w", err)
	}
	body, err := tt.Execute(facts, NotificationTextMax)
	if err != nil {
		return "", "", fmt.Errorf("text: %w", err)
	}
	return strings.Join(strings.Fields(sub), " "), strings.TrimSpace(body), nil
}

// --- the generated artifact --------------------------------------------------

type notificationFactsDoc struct {
	Comment     string        `json:"//"`
	Facts       []labelKeyDoc `json:"facts"`
	SubjectMax  int           `json:"subject_max"`
	TextMax     int           `json:"text_max"`
	Transitions []string      `json:"transitions"`
}

// NotificationFactsJSON renders the alarm fact map into
// docs/src/generated/notificationfacts.json (cmd/labelgen), the file the docs
// render the table a template author reads from. Same drift discipline as the
// label data map.
func NotificationFactsJSON() ([]byte, error) {
	doc := notificationFactsDoc{
		Comment:     "generated by make gen (cmd/labelgen); do not edit",
		Facts:       keyDocs(alarmFactKeys),
		SubjectMax:  NotificationSubjectMax,
		TextMax:     NotificationTextMax,
		Transitions: []string{TransitionRaised, TransitionCleared},
	}
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("notification facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}

// isForeignKeyViolation reports a delete or write a foreign key refused: a
// foreign_key_violation (23503), or a restrict_violation (23001), which is
// what an ON DELETE RESTRICT raises.
func isForeignKeyViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && (pgErr.SQLState() == "23503" || pgErr.SQLState() == "23001")
}
