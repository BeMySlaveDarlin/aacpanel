package collector

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
const publishInstructions = "When a question does not fit AskUserQuestion — over four questions or options, an " +
	"option that needs a paragraph, facts the choice rests on — or a finished analysis is worth reading on the " +
	"phone, publish a brief with brief_publish; also when the person asks for one " +
	`("send me a brief", "I will answer later"; in Russian "бриф", "опросник", "скинь в панель", "отвечу потом").`

const publishDescription = "Publishes a brief to the panel: a long piece the person reads on their phone or desk " +
	"and walks through — questions with what each rests on, the options and their cost, a place to answer — or " +
	"a finished piece of reading with no question in it. Before the first brief in a session, call this without " +
	"doc and read the rules it returns: the fields of the document, how its text reads, the ceilings. " +
	"With doc, the brief is published under doc.id; the same id from the same project updates it in place " +
	"and keeps the answers already given. The answers come back later as a message in this session, whenever " +
	"the person sends them: do not wait for them, let the turn end. check with doc reads the document and says " +
	"what is in it, publishing nothing. When the work stops until they answer, ask with AskUserQuestion " +
	"instead. A refusal says why nothing was published; say it in the conversation."

// deleteInstructions and deleteDescription are the removal's line and its own
// word.
const deleteInstructions = "When the person asks for a brief of this project to go, take it off the panel's " +
	"shelf with brief_delete."

const deleteDescription = "Takes a brief off the panel's shelf by its id, with the answers given to it: " +
	"published again under the same id, it starts empty. A session removes only the briefs of the directory " +
	"it works in; a brief from elsewhere is refused with the directory it belongs to. Remove one when the " +
	"person asks: a brief nobody answered is swept on its own after a month."

// maxBrief is the collector's ceiling on a publication, kept here too: a
// document over it is refused with the reason rather than cut off in the
// middle of the write.
const maxBrief = 4 << 20

// briefWait bounds one exchange with the collector: a brief of megabytes is
// read whole before it is answered.
const briefWait = 10 * time.Second

// HostGuide is where a machine keeps rules of its own for the text of a
// brief, added to the shipped ones when the file is there: the words a brief
// on this machine avoids, samples of briefs taken and refused.
func HostGuide() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "aacpanel", "brief-guide.md")
}

// Rules are how a brief is written: the shipped rules, then the host's own
// from guide when the file is there. It is read on every call, so an edit
// holds from the next one.
func Rules(guide string) string {
	if guide == "" {
		return rules
	}
	raw, err := os.ReadFile(guide)
	if errors.Is(err, fs.ErrNotExist) {
		return rules
	}
	if err != nil {
		return rules + "\nThis machine's own rules for a brief were not read: " + err.Error() + ".\n"
	}
	own := strings.TrimSpace(string(raw))
	if own == "" {
		return rules
	}
	return rules + "\n## The rules of this machine\n\nThe host this session runs on adds rules of its own. " +
		"Where they differ from the ones above, these hold.\n\n" + own + "\n"
}

// BriefPublish is the tool that publishes a brief through the collector on
// socket, with the rules of guide added to the shipped ones. It is allowed: a
// brief asks nothing of the person until they choose to open it.
func BriefPublish(socket, guide string) mcp.Tool {
	return mcp.Tool{
		Name:         PublishName,
		Title:        "Publish a brief",
		Description:  publishDescription,
		InputSchema:  publishSchema(),
		Instructions: publishInstructions,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return publish(ctx, socket, guide, bind, args)
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
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"doc": map[string]any{
				"type":        "object",
				"description": "The brief, as the rules describe it. Left out, the call returns the rules and publishes nothing.",
			},
			"check": map[string]any{
				"type":        "boolean",
				"description": "Read doc and say what is in it, publishing nothing.",
			},
		},
		"additionalProperties": false,
	}
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
func publish(ctx context.Context, socket, guide string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
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
		return Rules(guide), false
	}
	title, questions, asking := contents(doc)
	if args.Check {
		return fmt.Sprintf("%s: %d questions, %d of them ask something. Nothing was published; "+
			"call again without check to publish it.", title, questions, asking), false
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
	if asking == 0 {
		return "Published as " + got.ID + ": the person reads it in the panel.", false
	}
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
		return nil, errors.New("doc is the brief as a JSON object with a title and, if it asks anything, questions")
	}
	return raw, nil
}

// contents is what a check says of a document: its title, its questions, and
// how many of them ask something — a question of kind none is a block with a
// number on it.
func contents(doc json.RawMessage) (title string, questions, asking int) {
	var d struct {
		Title     any   `json:"title"`
		Questions []any `json:"questions"`
	}
	_ = json.Unmarshal(doc, &d)
	title, _ = d.Title.(string)
	if title == "" {
		title = "untitled"
	}
	for _, q := range d.Questions {
		if q, ok := q.(map[string]any); ok && q["kind"] != "none" {
			asking++
		}
	}
	return title, len(d.Questions), asking
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
