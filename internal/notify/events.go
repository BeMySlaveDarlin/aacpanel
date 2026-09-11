package notify

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	agentSilence = 2 * time.Minute
	longTurn     = 10 * time.Minute
	limitLoud    = 80
)

// DomainSession, DomainContainer, DomainStore and DomainVision are the event classes.
const (
	DomainSession   = "session"
	DomainContainer = "container"
	DomainStore     = "store"
	DomainVision    = "vision"
	domainLimit     = "limit"
)

// Event is a reason to notify, with the wording for both raising and clearing it.
type Event struct {
	Key      string   `json:"key"`
	Domain   string   `json:"domain"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Severity Severity `json:"severity"`

	GoneTitle    string   `json:"goneTitle,omitempty"`
	GoneBody     string   `json:"goneBody,omitempty"`
	GoneSeverity Severity `json:"goneSeverity,omitempty"`

	Session string `json:"session,omitempty"`
}

func (e Event) raised() Message {
	return Message{Title: e.Title, Body: e.Body, Tag: e.Key, Severity: e.Severity, Session: e.Session}
}

func (e Event) gone() Message {
	sev := e.GoneSeverity
	if sev == "" {
		sev = Info
	}
	return Message{Title: e.GoneTitle, Body: e.GoneBody, Tag: e.Key, Severity: sev, Session: e.Session}
}

// World is what the panel sees about the host at one moment.
type World struct {
	Now time.Time

	Sessions   []Session
	AgentAge   time.Duration
	AgentErr   string
	Limits     []Limit
	LimitsSeen bool

	Containers []Container
	Stacks     []Stack
	DockerErr  string

	Alerts []Alert
	Probes []Probe
	DBOff  bool
	DBErr  string

	PanelDid map[string]bool
}

// Session is a live claude session as the agent snapshot shows it.
type Session struct {
	ID         string
	Name       string
	Profile    string
	CWD        string
	Status     string
	StatusAt   int64
	WaitingFor string
	Ask        *Ask
}

// Ask is a question asked by a session.
type Ask struct {
	Header string
	Text   string
	Count  int
	At     string
}

func (s Session) key() string {
	if s.ID != "" {
		return s.ID
	}
	return "name:" + s.Name
}

func (s Session) where() string {
	parts := make([]string, 0, 2)
	if s.Profile != "" {
		parts = append(parts, s.Profile)
	}
	if base := baseName(s.CWD); base != "" {
		parts = append(parts, base)
	}
	return strings.Join(parts, " · ")
}

// Container is a docker container as the panel sees it.
type Container struct {
	Name   string
	Stack  string
	State  string
	Status string
	Health string
}

func (c Container) running() bool { return c.State == "running" }

// Stack is a compose stack: how many of its containers run out of how many.
type Stack struct {
	Name    string
	Running int
	Total   int
}

// Alert is an open alert of the rules engine together with what opened it.
type Alert struct {
	ID        int64
	Rule      string
	Subject   string
	Severity  string
	Value     *float64
	Threshold float64
	Op        string
	Unit      string
	ForSec    int
}

// Probe is an availability check with its latest result.
type Probe struct {
	ID      int
	Name    string
	Target  string
	OK      bool
	Outcome string
	Error   string
	Streak  int
}

// Limit is the subscription window usage of one contour.
type Limit struct {
	Contour  string
	Window   string
	Pct      int
	ResetsAt int64
}

// Report is what Look saw on this pass.
type Report struct {
	Raise []Event
	Hold  []string
	Drop  []string
	Seen  []string
}

// Look compares two neighbouring views of the host and reports what is worth saying.
func Look(prev, cur World) Report {
	var r Report

	agentBlind := cur.AgentErr != "" || cur.AgentAge > agentSilence
	dbBlind := !cur.DBOff && cur.DBErr != ""

	r.Seen = append(r.Seen, DomainVision)
	switch {
	case cur.AgentErr != "":
		r.raise(blindAgent("The host snapshot cannot be read: " + cur.AgentErr))
	case cur.AgentAge > agentSilence:
		r.raise(blindAgent("The host snapshot has not updated for " + dur(cur.AgentAge) +
			" — metrics, sessions and questions are frozen."))
	}
	if dbBlind {
		r.raise(blindDB(cur.DBErr))
	}

	if !agentBlind {
		r.Seen = append(r.Seen, DomainSession)
		sessions(&r, prev, cur)
	}
	if !agentBlind && cur.LimitsSeen {
		limits(&r, cur)
	}
	if cur.DockerErr == "" {
		r.Seen = append(r.Seen, DomainContainer)
		containers(&r, prev, cur)
	}
	if !cur.DBOff && cur.DBErr == "" {
		r.Seen = append(r.Seen, DomainStore)
		alerts(&r, cur)
		probes(&r, cur)
	}
	return r
}

func (r *Report) raise(e Event) {
	r.Raise = append(r.Raise, e)
	r.Hold = append(r.Hold, e.Key)
}

func (r *Report) once(e Event) {
	r.Raise = append(r.Raise, e)
}

func sessions(r *Report, prev, cur World) {
	was := map[string]Session{}
	for _, s := range prev.Sessions {
		was[s.key()] = s
	}

	for _, s := range cur.Sessions {
		before, seen := was[s.key()]
		switch {
		case s.Ask != nil:
			r.raise(asked(s))
		case s.Status == "waiting" && seen && before.Status == "waiting" &&
			before.StatusAt == s.StatusAt && before.Ask == nil:
			r.raise(waiting(s))
		}

		if seen && before.Status == "busy" && s.Status == "idle" &&
			before.StatusAt > 0 && s.StatusAt > before.StatusAt {
			took := time.Duration(s.StatusAt-before.StatusAt) * time.Millisecond
			if took >= longTurn {
				r.once(freed(s, took))
			}
		}
		delete(was, s.key())
	}

	if prev.AgentErr != "" || prev.AgentAge > agentSilence || len(prev.Sessions) == 0 {
		return
	}
	for _, s := range was {
		if cur.PanelDid["session:"+s.Name] {
			continue
		}
		r.once(closed(s))
	}
}

func asked(s Session) Event {
	body := s.Ask.Text
	if s.Ask.Header != "" {
		body = s.Ask.Header + ": " + s.Ask.Text
	}
	if s.Ask.Count > 1 {
		body += fmt.Sprintf(" · and %d more in this round", s.Ask.Count-1)
	}
	if where := s.where(); where != "" {
		body += " · " + where
	}
	return Event{
		Key:      "ask:" + s.key() + ":" + s.Ask.At,
		Domain:   DomainSession,
		Session:  s.Name,
		Title:    "Question · " + s.Name,
		Body:     body,
		Severity: Critical,
	}
}

func waitReason(reason string) string {
	switch reason {
	case "dialog open":
		return "a dialog is open"
	case "input needed":
		return "input needed"
	case "sandbox request":
		return "asking to leave the sandbox"
	case "goal proposal":
		return "proposing a goal"
	case "worker request":
		return "a subagent is asking"
	case "":
		return "reason not given"
	default:
		return reason
	}
}

func waiting(s Session) Event {
	body := upperFirst(waitReason(s.WaitingFor)) + " — the session is blocked until you answer"
	if where := s.where(); where != "" {
		body += " · " + where
	}
	return Event{
		Key:      "wait:" + s.key() + ":" + strconv.FormatInt(s.StatusAt, 10),
		Domain:   DomainSession,
		Session:  s.Name,
		Title:    "Waiting for permission · " + s.Name,
		Body:     body,
		Severity: Critical,
	}
}

func freed(s Session, took time.Duration) Event {
	body := "The turn took " + dur(took)
	if where := s.where(); where != "" {
		body += " · " + where
	}
	return Event{
		Key:      "done:" + s.key() + ":" + strconv.FormatInt(s.StatusAt, 10),
		Domain:   DomainSession,
		Session:  s.Name,
		Title:    "Turn finished · " + s.Name,
		Body:     body,
		Severity: Info,
	}
}

func closed(s Session) Event {
	body := "Closed outside the panel: it crashed or was closed at the machine"
	if where := s.where(); where != "" {
		body += " · " + where
	}
	return Event{
		Key:      "gone:" + s.key(),
		Domain:   DomainSession,
		Session:  s.Name,
		Title:    "Session closed · " + s.Name,
		Body:     body,
		Severity: Warning,
	}
}

func containers(r *Report, prev, cur World) {
	was := map[string]Container{}
	for _, c := range prev.Containers {
		was[c.Name] = c
	}

	for _, c := range cur.Containers {
		before, seen := was[c.Name]
		switch {
		case !c.running() && seen && before.running() && !cur.PanelDid["container:"+c.Name]:
			r.raise(fell(c))
		case !c.running():
			r.Hold = append(r.Hold, "container:"+c.Name)
		}
		switch {
		case c.Health == "unhealthy" && !cur.PanelDid["container:"+c.Name]:
			r.raise(sick(c))
		case c.Health == "unhealthy":
			r.Hold = append(r.Hold, "health:"+c.Name)
		}
		delete(was, c.Name)
	}
	for name := range was {
		r.Drop = append(r.Drop, "container:"+name, "health:"+name)
	}

	stacks(r, prev, cur)
}

func stacks(r *Report, prev, cur World) {
	was := map[string]Stack{}
	for _, s := range prev.Stacks {
		was[s.Name] = s
	}
	for _, s := range cur.Stacks {
		before, seen := was[s.Name]
		switch {
		case s.Total > 0 && s.Running == 0 && seen && before.Running > 0 && !cur.PanelDid["stack:"+s.Name]:
			r.raise(wentDark(s))
		case s.Total > 0 && s.Running == 0:
			r.Hold = append(r.Hold, "stack:"+s.Name)
		}
		delete(was, s.Name)
	}
	for name := range was {
		r.Drop = append(r.Drop, "stack:"+name)
	}
}

func fell(c Container) Event {
	body := c.Status
	if body == "" {
		body = "state: " + c.State
	}
	body += stackTail(c.Stack)
	return Event{
		Key:          "container:" + c.Name,
		Domain:       DomainContainer,
		Title:        "Container down · " + c.Name,
		Body:         body,
		Severity:     Critical,
		GoneTitle:    "Container up · " + c.Name,
		GoneBody:     "Running again" + stackTail(c.Stack),
		GoneSeverity: Info,
	}
}

func sick(c Container) Event {
	return Event{
		Key:          "health:" + c.Name,
		Domain:       DomainContainer,
		Title:        "Container unhealthy · " + c.Name,
		Body:         "healthcheck failing" + stackTail(c.Stack) + " · " + c.Status,
		Severity:     Warning,
		GoneTitle:    "Container healthy · " + c.Name,
		GoneBody:     "healthcheck passing again" + stackTail(c.Stack),
		GoneSeverity: Info,
	}
}

func wentDark(s Stack) Event {
	return Event{
		Key:          "stack:" + s.Name,
		Domain:       DomainContainer,
		Title:        "Stack down · " + s.Name,
		Body:         fmt.Sprintf("None of its %d containers is running, and the panel did not stop it.", s.Total),
		Severity:     Critical,
		GoneTitle:    "Stack up · " + s.Name,
		GoneBody:     fmt.Sprintf("%d of %d running.", s.Running, s.Total),
		GoneSeverity: Info,
	}
}

func stackTail(stack string) string {
	if stack == "" {
		return ""
	}
	return " · stack " + stack
}

func alerts(r *Report, cur World) {
	for _, a := range cur.Alerts {
		r.raise(tripped(a))
	}
}

func tripped(a Alert) Event {
	body := "the rule tripped"
	switch {
	case a.Unit == unitFlag, a.Unit == unitFlagWas:
		body = "the flag is raised"
	case a.Value != nil:
		body = fmt.Sprintf("now %s, threshold %s %s",
			amount(*a.Value, a.Unit), a.Op, amount(a.Threshold, a.Unit))
	}
	if a.ForSec > 0 {
		body += " · lasting " + dur(time.Duration(a.ForSec)*time.Second)
	}
	return Event{
		Key:          "alert:" + strconv.FormatInt(a.ID, 10),
		Domain:       DomainStore,
		Title:        a.Rule + " · " + a.Subject,
		Body:         upperFirst(body),
		Severity:     severityOf(a.Severity),
		GoneTitle:    "Alert cleared · " + a.Subject,
		GoneBody:     a.Rule + " — back to normal.",
		GoneSeverity: Info,
	}
}

func severityOf(s string) Severity {
	switch Severity(s) {
	case Info:
		return Info
	case Critical:
		return Critical
	default:
		return Warning
	}
}

func probes(r *Report, cur World) {
	for _, p := range cur.Probes {
		if p.OK {
			continue
		}
		r.raise(broken(p))
	}
}

func broken(p Probe) Event {
	title := "Not answering · " + p.Name
	if p.Outcome == "degraded" {
		title = "Works worse · " + p.Name
	}
	body := outcomeText(p.Outcome)
	if p.Error != "" {
		body += ": " + p.Error
	}
	if p.Streak > 1 {
		body += fmt.Sprintf(" · %d failures in a row", p.Streak)
	}
	if p.Target != "" {
		body += " · " + p.Target
	}
	return Event{
		Key:          "probe:" + strconv.Itoa(p.ID),
		Domain:       DomainStore,
		Title:        title,
		Body:         upperFirst(body),
		Severity:     Warning,
		GoneTitle:    "Answers again · " + p.Name,
		GoneBody:     "The probe passes again.",
		GoneSeverity: Info,
	}
}

func outcomeText(outcome string) string {
	switch outcome {
	case "network":
		return "the network did not get through"
	case "timeout":
		return "did not answer in time"
	case "status":
		return "answered with an error"
	case "degraded":
		return "works worse than usual"
	case "config":
		return "the probe is set up wrong"
	default:
		return "failed"
	}
}

func blindAgent(body string) Event {
	return Event{
		Key:          "blind:agent",
		Domain:       DomainVision,
		Title:        "Panel is blind · agent",
		Body:         body,
		Severity:     Warning,
		GoneTitle:    "Panel sees again · agent",
		GoneBody:     "The host snapshot is updating.",
		GoneSeverity: Info,
	}
}

func blindDB(reason string) Event {
	body := "History, alerts and the action log are unavailable."
	if reason != "" {
		body = "History, alerts and the action log are unavailable: " + reason
	}
	return Event{
		Key:          "blind:db",
		Domain:       DomainVision,
		Title:        "Panel is blind · database",
		Body:         body,
		Severity:     Warning,
		GoneTitle:    "Panel sees again · database",
		GoneBody:     "History and the action log are back.",
		GoneSeverity: Info,
	}
}

func limits(r *Report, cur World) {
	for _, l := range cur.Limits {
		if l.Window == "" || l.Contour == "" {
			continue
		}
		watch := domainLimit + ":" + l.Window + ":" + l.Contour
		r.Seen = append(r.Seen, watch)

		key := watch + ":" + strconv.FormatInt(l.ResetsAt, 10)
		switch {
		case l.ResetsAt > 0 && !cur.Now.Before(time.Unix(l.ResetsAt, 0)):
		case l.Pct >= limitLoud:
			r.raise(burning(cur.Now, l, watch, key))
		default:
			r.Hold = append(r.Hold, key)
		}
	}
}

func burning(now time.Time, l Limit, watch, key string) Event {
	body := fmt.Sprintf("%d%% used", l.Pct)
	if left := time.Unix(l.ResetsAt, 0).Sub(now); l.ResetsAt > 0 && left > 0 {
		body += " · the window resets in " + dur(left)
	}
	return Event{
		Key:          key,
		Domain:       watch,
		Title:        upperFirst(windowText(l.Window)) + " limit · " + l.Contour,
		Body:         body + ".",
		Severity:     Warning,
		GoneTitle:    "Limit reset · " + l.Contour,
		GoneBody:     "The " + windowText(l.Window) + " window has started over.",
		GoneSeverity: Info,
	}
}

func windowText(window string) string {
	switch window {
	case "5h":
		return "5-hour"
	case "7d":
		return "weekly"
	default:
		return window
	}
}

func dur(d time.Duration) string {
	sec := int(d.Round(time.Second).Seconds())
	switch {
	case sec < 60:
		return strconv.Itoa(sec) + " s"
	case sec < 3600:
		return strconv.Itoa(sec/60) + " min"
	case sec < 24*3600:
		return fmt.Sprintf("%d h %d m", sec/3600, (sec%3600)/60)
	default:
		return fmt.Sprintf("%d d %d h", sec/(24*3600), (sec%(24*3600))/3600)
	}
}

func amount(v float64, unit string) string {
	switch unit {
	case "%":
		return trimZero(v) + "%"
	case unitBytes, unitBytesWas:
		return bytesText(v)
	default:
		return trimZero(v)
	}
}

const (
	unitBytes    = "bytes"
	unitBytesWas = "байты"
	unitFlag     = "flag"
	unitFlagWas  = "флаг"
)

func trimZero(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

var byteUnits = []string{"B", "KB", "MB", "GB", "TB"}

func bytesText(v float64) string {
	unit := 0
	for v >= 1024 && unit < len(byteUnits)-1 {
		v /= 1024
		unit++
	}
	if unit > 0 && v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " " + byteUnits[unit]
	}
	return strconv.FormatFloat(v, 'f', 0, 64) + " " + byteUnits[unit]
}

func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func upperFirst(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}
