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
const secretInstructions = "When the work needs a credential — a token, a password, a key — or the person offers " +
	`one ("I will send the token"; in Russian "скину токен", "дам пароль", "вот доступы"), ask for it with ` +
	"secret_ask, never in the conversation: the value lands in a file and you get its path."

const secretDescription = "Asks the person for credentials without the values entering the conversation. " +
	"The panel shows a card in this conversation and, on their phone or desk, a notepad prefilled with " +
	"template; they fill it in and save, and the host writes it to a file only its owner reads. A message with " +
	"the path comes into this session when they save it, possibly much later: do not wait for it, let the turn " +
	"end. Use the file by its path — --env-file, < file, cp, source, --password-stdin — and never read, cat, " +
	"grep or print it: whatever is read goes into the transcript. name is the file; the same name replaces the " +
	"secret, which is how a token is rotated. template is KEY= lines with the values left empty and # comments " +
	"on where to find each one. A refusal says which argument is wrong; nothing was asked then."

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
