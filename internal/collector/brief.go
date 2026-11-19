package collector

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/mcp"
)

// The brief tools' names on the panel's server: the model calls them
// mcp__aacpanel__brief_publish and mcp__aacpanel__brief_delete.
const (
	PublishName = "brief_publish"
	DeleteName  = "brief_delete"
)

// rules are how a brief is written, as a call without a document returns
// them. They are not in the description: they run to some ten kilobytes, and
// a session that never writes a brief would carry them for nothing.
//
//go:embed brief-rules.md
var rules string

// publishInstructions is the tool's line in the server's word to every
// session: the model sees it and the name, so it says when to reach for the
// tool. publishDescription is read once the tool is looked up.
const publishInstructions = "Use brief_publish for a task with decisions to answer later " +
	`("send me a brief"; "бриф", "опросник", "скинь в панель", "отвечу потом"); long reading goes through file_send; a blocking choice goes to AskUserQuestion.`

const publishDescription = "Publishes a task, why it matters, and decisions with options the person answers later in the panel. " +
	"Each option carries a reason, cost and agent hours; mark the recommended option, or options for multiple choice. " +
	"Long reading without decisions goes through file_send. Before the first publication, call without doc and read the rules. " +
	"Pass the document as doc; check: true counts decisions and options without publishing. The document is at most 4 MB. " +
	"The same doc.id in the same project updates the brief and keeps its answers. Once answers are sent, a reissue " +
	"does not open it again: new decisions need a new brief under a new id. Answers arrive as one message; " +
	"continue independent work or end the turn, never poll or wait here. When work stops until they answer, " +
	"ask with AskUserQuestion instead. Use ask for a question while work continues and secret_ask for credentials. " +
	"A refusal means nothing was published; report it in the conversation."

const codexPublishInstructions = "Use brief_publish for a task with decisions to answer later " +
	`("send me a brief"; "бриф", "опросник", "скинь в панель", "отвечу потом"); long reading goes through file_send; use the client's question tool for a blocking choice.`

var codexPublishDescription = strings.Replace(publishDescription, "ask with AskUserQuestion instead",
	"use the client's question tool; if unavailable, ask in the conversation and end the turn", 1)

// codexRules goes before the rules a thread of codex reads: they are written
// for claude, whose question tool has a different name.
const codexRules = "This session is a thread of codex. Where the rules below name AskUserQuestion, read a " +
	"question through the client's question tool; if unavailable, ask in the conversation and end the turn.\n\n"

// deleteInstructions and deleteDescription are the removal's line and its own
// word.
const deleteInstructions = "Use brief_delete when the person asks to remove a published brief of this project."

const deleteDescription = "Removes a published brief from the panel's shelf. Pass its id; only briefs of this " +
	"session's directory can be removed, and a brief from elsewhere is refused. Republishing a removed id " +
	"starts with no answers. Use only when the person asks to remove it, not to clear an unanswered brief or " +
	"make it ask again. To revise a brief use brief_publish with the same id; for new questions after answers " +
	"were sent, publish under a new id. An unanswered brief is swept after a month. After success, report the " +
	"removal and continue; this call waits for no answer."

// maxBrief is the collector's ceiling on a publication, kept here too: a
// document over it is refused with the reason rather than cut off in the
// middle of the write.
const maxBrief = 4 << 20

// briefWait bounds one exchange with the collector: a brief of megabytes is
// read whole before it is answered.
const briefWait = 10 * time.Second

// Rules appends the guide kept by the panel to the shipped rules.
func Rules(guide string) string {
	own := strings.TrimSpace(guide)
	if own == "" {
		return rules
	}
	return rules + "\n## The rules of this machine\n\nThe host this session runs on adds rules of its own. " +
		"Where they differ from the ones above, these hold.\n\n" + own + "\n"
}

// A call reads the current guide, without a cached copy: after an edit, an
// old instruction must not silently win. The shipped rules still work while
// the panel is down, and the missing guide is said in the answer.
func panelRules(ctx context.Context, panel string) string {
	if panel == "" {
		return rules
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	text, err := readGuide(ctx, panel)
	if err != nil {
		return rules + "\nThis machine's own rules for a brief were not read from the panel this time: " + err.Error() + ".\n"
	}
	return Rules(text)
}

func readGuide(ctx context.Context, panel string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(panel, "/")+"/api/settings/brief-guide", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the panel answered %s", resp.Status)
	}
	var body struct {
		Text *string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Text == nil {
		return "", errors.New("the panel did not return a brief guide")
	}
	return *body.Text, nil
}

// BriefPublish is the tool that publishes a brief through the collector on
// socket, with the guide read from the panel. It is allowed: a
// brief asks nothing of the person until they choose to open it.
func BriefPublish(socket, panel string) mcp.Tool {
	return briefPublish(socket, publishInstructions, publishDescription, func(ctx context.Context) string { return panelRules(ctx, panel) })
}

// CodexBriefPublish is the same tool for a thread of codex, told how a brief
// differs from a question in codex's terms.
func CodexBriefPublish(socket, panel string) mcp.Tool {
	return briefPublish(socket, codexPublishInstructions, codexPublishDescription,
		func(ctx context.Context) string { return codexRules + panelRules(ctx, panel) })
}

func briefPublish(socket, instructions, description string, rules func(context.Context) string) mcp.Tool {
	return mcp.Tool{
		Name:         PublishName,
		Title:        "Publish a brief",
		Description:  description,
		InputSchema:  publishSchema(),
		Instructions: instructions,
		Standing:     briefStanding,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return publish(ctx, socket, rules, bind, args)
		},
	}
}

// BriefDelete is the tool that takes a brief of the session's directory off
// the shelf through the collector on socket. It is allowed: it removes only
// what a session of the same directory published.
func BriefDelete(socket string) mcp.Tool {
	return mcp.Tool{
		Name:         DeleteName,
		Title:        "Remove a brief",
		Description:  deleteDescription,
		InputSchema:  deleteSchema(),
		Instructions: deleteInstructions,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return drop(ctx, socket, bind, args)
		},
	}
}

func publishSchema() map[string]any {
	text := func(limit int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": limit}
	}
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": append([]string{}, required...), "additionalProperties": false}
	}
	option := object(map[string]any{
		"key": text(4), "label": text(120), "reason": text(400), "cost": text(300),
		"hours":       map[string]any{"type": "number", "minimum": 0.1, "maximum": 1000},
		"recommended": map[string]any{"type": "boolean"},
	}, "label", "reason", "cost", "hours")
	decision := object(map[string]any{
		"id": text(40), "title": text(120), "ask": text(600), "multi": map[string]any{"type": "boolean"},
		"options": map[string]any{"type": "array", "minItems": 2, "maxItems": 8, "items": option},
	}, "title", "options")
	doc := object(map[string]any{
		"id":    map[string]any{"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,63}$"},
		"title": text(120), "task": text(400), "why": text(800),
		"context": map[string]any{"type": "array", "maxItems": 6,
			"items": object(map[string]any{"title": text(80), "text": text(2000)}, "title", "text")},
		"decisions": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": decision},
	}, "id", "title", "task", "why", "decisions")
	doc["description"] = "The task and its decisions. Left out, the call returns the rules and publishes nothing."
	return object(map[string]any{"doc": doc, "check": map[string]any{"type": "boolean",
		"description": "Count the decisions and options, publishing nothing."}})
}

func deleteSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"pattern":     "^[a-z0-9][a-z0-9-]{0,63}$",
				"description": "The id the brief was published under.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

// publish returns the rules to a call without a document, says what is in
// one to a check, and hands any other to the collector under the session and
// the directory of the claude the server serves.
func publish(ctx context.Context, socket string, rules func(context.Context) string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Doc   json.RawMessage `json:"doc"`
		Check bool            `json:"check"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "The brief was not published: the arguments are not a brief's (" + err.Error() + ").", true
		}
	}
	doc, err := document(args.Doc)
	if err != nil {
		return "The brief was not published: " + err.Error() + ".", true
	}
	if doc == nil {
		return rules(ctx), false
	}
	title, decisions, options := contents(doc)
	if args.Check {
		return fmt.Sprintf("%s: %d decisions, %d options. Nothing was published; "+
			"call again without check to publish it.", title, decisions, options), false
	}

	b, err := session(bind)
	if err != nil {
		return "The brief was not published: " + err.Error() + ".", true
	}
	request := struct {
		SessionID string          `json:"sessionId"`
		CWD       string          `json:"cwd"`
		Doc       json.RawMessage `json:"doc"`
	}{b.SessionID, b.Place.Dir, doc}
	body, err := json.Marshal(request)
	if err != nil {
		return "The brief was not published: " + err.Error() + ".", true
	}
	if len(body) > maxBrief {
		return fmt.Sprintf("The brief was not published: the document is longer than %d MB.", maxBrief>>20), true
	}
	got, err := ask(ctx, socket, briefWait, body)
	if err != nil {
		return "The brief was not published: " + err.Error() + ". Ask in the conversation instead.", true
	}
	if !got.OK {
		return "The brief was not published: " + refusal(got, "the collector refused it") + ".", true
	}
	// The feed draws the card of the brief from the start of this answer,
	// "Published as <id>" and a colon or a full stop (agent/chat/cards.py).
	return "Published as " + got.ID + ". The answers arrive in this session as a message when the person " +
		"sends them, possibly hours from now: do not wait for them.", false
}

// document is the brief the model sent: an object, or nothing when doc was
// left out. A client that hands a nested object over as a string of JSON is
// taken at its word.
func document(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		raw = bytes.TrimSpace([]byte(text))
	}
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
		return nil, errors.New("doc is the brief as a JSON object with title, task, why and decisions")
	}
	var value any
	_ = json.Unmarshal(raw, &value)
	if oldBriefForm(value) {
		return nil, errors.New(oldBriefMessage)
	}
	return raw, nil
}

const oldBriefMessage = "this is the old form of a brief: a brief is a task, why, and decisions with options — call brief_publish without doc for the rules."

func oldBriefForm(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "eyebrow", "lede", "summary", "lineage", "sections", "closing", "questions", "facts", "read", "chips", "capture":
				return true
			}
			if oldBriefForm(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if oldBriefForm(child) {
				return true
			}
		}
	}
	return false
}

// contents counts choices without publishing or requiring a live session.
func contents(doc json.RawMessage) (title string, decisions, options int) {
	var d struct {
		Title     string `json:"title"`
		Decisions []struct {
			Options []json.RawMessage `json:"options"`
		} `json:"decisions"`
	}
	_ = json.Unmarshal(doc, &d)
	title = d.Title
	if title == "" {
		title = "untitled"
	}
	for _, decision := range d.Decisions {
		options += len(decision.Options)
	}
	return title, len(d.Decisions), options
}

// drop asks the collector to take a brief off the shelf, held to the
// directory of the claude the server serves.
func drop(ctx context.Context, socket string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		ID string `json:"id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was removed: the arguments are not a brief's id (" + err.Error() + ").", true
		}
	}
	if args.ID == "" {
		return "Nothing was removed: name the brief by the id it was published under.", true
	}
	b, err := session(bind)
	if err != nil {
		return "The brief was not removed: " + err.Error() + ".", true
	}
	body, err := json.Marshal(map[string]string{"drop": args.ID, "sessionId": b.SessionID, "cwd": b.Place.Dir})
	if err != nil {
		return "The brief was not removed: " + err.Error() + ".", true
	}
	got, err := ask(ctx, socket, briefWait, body)
	if err != nil {
		return "The brief was not removed: " + err.Error() + ".", true
	}
	if !got.OK {
		return "The brief was not removed: " + refusal(got, "the collector refused to remove it") + ".", true
	}
	return args.ID + " is off the shelf, with the answers given to it.", false
}
