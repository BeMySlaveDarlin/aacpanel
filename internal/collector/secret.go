package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

// SecretAskName is the notepad tool's name on the panel's server: the model
// calls it mcp__aacpanel__secret_ask.
const SecretAskName = "secret_ask"

// secretInstructions is the tool's line in the server's word to every
// session; secretDescription is read once the tool is looked up.
const secretInstructions = "For a needed or offered credential, use secret_ask " +
	`("I will send the token"; "скину токен", "дам пароль", "вот доступы"); use the file path and keep values out of chat.`

const secretDescription = "Opens a credential notepad in the panel and calls the phone. The person saves its " +
	"values to a file only its owner reads; this session receives a message with the path, never the values. " +
	"Required name is 1-64 characters of a-z, 0-9, '.', '_' or '-', starting with a letter or digit; reusing " +
	"it replaces that secret. Required title says what is needed in 1-120 characters. Optional template is " +
	"at most 8192 bytes: empty KEY= lines and # comments on where to find each value. Do not put secrets in " +
	"any argument, question dialog, brief, notify or conversation. Use the client's question tool for " +
	"ordinary choices; do not add notify here, the tool calls already. Do not poll or wait in this turn: " +
	"continue independent work, then let the turn end. Once the path arrives, pass it to --env-file, " +
	"--password-stdin, input redirection or a file copy; never read, cat, grep or print its contents into " +
	"the transcript. If the phone call fails the card still waits; invalid arguments create no request."

// The arguments the notepad takes, as the panel holds them.
const (
	secretTitleMax    = 120
	secretTemplateMax = 8192
)

// secretName is the name of a secret, the same rule as the executor's: a
// file of the directory of secrets that is neither hidden nor a way out.
var secretName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// SecretAsk is the tool that asks the person for a secret. It is allowed: it
// only puts a card in the feed and calls the phone, and what the person types
// never comes back through it. The card is drawn from the call and its
// answer, so the panel keeps nothing of the request; the call to the phone
// goes through the collector on socket.
func SecretAsk(socket string) mcp.Tool {
	return mcp.Tool{
		Name:         SecretAskName,
		Title:        "Ask for a secret",
		Description:  secretDescription,
		InputSchema:  secretSchema(),
		Instructions: secretInstructions,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return askSecret(ctx, socket, bind, args)
		},
	}
}

func secretSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"pattern":     secretName.String(),
				"description": "The file the secret is saved as, e.g. github-token or evirma-db.env.",
			},
			"title": map[string]any{
				"type":        "string",
				"minLength":   1,
				"maxLength":   secretTitleMax,
				"description": "What is asked, shown on the card and in the push: \"GitHub token to push the release\".",
			},
			"template": map[string]any{
				"type":        "string",
				"description": "The notepad the person starts from, up to 8192 bytes: KEY= lines, # comments on where to find each value.",
			},
		},
		"required":             []string{"name", "title"},
		"additionalProperties": false,
	}
}

// askSecret checks the arguments and calls the person to the conversation
// the server serves. The answer starts with the name the card is drawn by
// (agent/chat/cards.py); a refusal of the phone call does not take the card
// away, so it is said and the call stands.
func askSecret(ctx context.Context, socket string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Name     string `json:"name"`
		Title    string `json:"title"`
		Template string `json:"template"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was asked: the arguments are not a secret's (" + err.Error() + ").", true
		}
	}
	if !secretName.MatchString(args.Name) {
		return "Nothing was asked: name is the file of the secret, 1 to 64 characters of a-z, 0-9, '.', '_' and " +
			"'-', starting with a letter or a digit.", true
	}
	title := strings.Join(strings.Fields(args.Title), " ")
	if title == "" || utf8.RuneCountInString(title) > secretTitleMax {
		return fmt.Sprintf("Nothing was asked: title says what is asked, in 1 to %d characters.", secretTitleMax), true
	}
	if len(args.Template) > secretTemplateMax {
		return fmt.Sprintf("Nothing was asked: template is %d bytes, over the %d a notepad starts from.",
			len(args.Template), secretTemplateMax), true
	}
	b, err := session(bind)
	if err != nil {
		return "Nothing was asked: " + err.Error() + ".", true
	}

	said := "Asked as " + args.Name + ".\nThe person fills the notepad in the panel; a message with the path comes " +
		"into this session when it is saved. Do not wait for it in this turn. Use the file by its path " +
		"(--env-file, < file, cp, source, --password-stdin) and never read, cat, grep or print it: whatever is " +
		"read goes into the transcript."
	body, err := json.Marshal(map[string]string{"sessionId": b.SessionID, "text": "Asks for a secret: " + title})
	if err != nil {
		return said + "\nTheir phone was not called: " + err.Error() + ".", false
	}
	got, err := ask(ctx, socket, callWait, body)
	switch {
	case err != nil:
		said += "\nTheir phone was not called: " + err.Error() + "; the card waits in the conversation."
	case !got.OK:
		said += "\nTheir phone was not called: " + refusal(got, "the collector refused the call") +
			"; the card waits in the conversation."
	}
	return said, false
}
