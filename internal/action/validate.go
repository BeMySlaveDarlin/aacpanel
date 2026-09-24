package action

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ErrBadRequest means the request is not acceptable.
type ErrBadRequest struct{ msg string }

func (e ErrBadRequest) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return ErrBadRequest{msg: fmt.Sprintf(format, args...)}
}

// Validate checks the request before it reaches anything that executes.
func (r Request) Validate() error {
	if r.Ask != "" {
		if r.Kind != "" || r.Resume != "" {
			return badRequest("question %q performs no actions", r.Ask)
		}
		switch r.Ask {
		case AskKinds:
			if r.Target != "" {
				return badRequest("question %q has no target", r.Ask)
			}
		case AskPermission, AskWindow, AskModels:
			if r.Target == "" {
				return badRequest("question %q without a session name", r.Ask)
			}
			if len(r.Target) > targetMax {
				return badRequest("the session name is longer than %d characters", targetMax)
			}
			if err := safeTarget(r.Target); err != nil {
				return err
			}
		default:
			return badRequest("unknown question %q", r.Ask)
		}
		return nil
	}
	if r.ID == "" {
		return badRequest("request without an id")
	}
	if len(r.ID) > idMax {
		return badRequest("the id is longer than %d characters", idMax)
	}
	if !safeID(r.ID) {
		return badRequest("the id contains forbidden characters")
	}
	if !Valid(r.Kind) {
		return badRequest("unknown action %q", r.Kind)
	}
	if r.Target == "" {
		return badRequest("action %s without a target", r.Kind)
	}
	targetLimit := targetMax
	if r.Kind == ProjectCreate {
		targetLimit = pathMax
	}
	if len(r.Target) > targetLimit {
		return badRequest("the target is longer than %d characters", targetLimit)
	}
	switch {
	case r.Kind == SessionPermit:
		if r.Permit == nil {
			return badRequest("action %s without the item to press", r.Kind)
		}
		if r.Permit.Option < 1 || r.Permit.Option > askOptionsMax {
			return badRequest("permission item %d is outside the range 1..%d", r.Permit.Option, askOptionsMax)
		}
		if r.Permit.Fingerprint == "" {
			return badRequest("a keypress without a dialog fingerprint: there would be nothing to check it against")
		}
		if len(r.Permit.Fingerprint) > PermitFingerprintMax {
			return badRequest("the fingerprint is longer than %d characters", PermitFingerprintMax)
		}
		if !safeID(r.Permit.Fingerprint) {
			return badRequest("the fingerprint contains forbidden characters")
		}
	case r.Kind == SessionAnswer, r.Kind == SessionDismiss:
		if r.Answer == nil {
			return badRequest("action %s without the question it belongs to", r.Kind)
		}
		if r.Answer.AskID == "" || !safeID(r.Answer.AskID) {
			return badRequest("an answer to a question without a usable id")
		}
		if len(r.Answer.AskID) > idMax {
			return badRequest("the question id is longer than %d characters", idMax)
		}
		if r.Kind == SessionDismiss {
			if len(r.Answer.Picks) > 0 || len(r.Answer.Texts) > 0 || len(r.Answer.Notes) > 0 {
				return badRequest("dismissing a question carries no answer: it interrupts the round instead of answering")
			}
			break
		}
		if len(r.Answer.Picks) == 0 {
			return badRequest("the answer is empty: neither picked options nor words of your own")
		}
		if len(r.Answer.Picks) > askQuestionsMax {
			return badRequest("the answer holds more than %d questions", askQuestionsMax)
		}
		for i, picks := range r.Answer.Picks {
			if len(picks) > askOptionsMax {
				return badRequest("question %d has more than %d options picked", i+1, askOptionsMax)
			}
			for _, pick := range picks {
				if pick < 1 || pick > askOptionsMax {
					return badRequest("question %d has a non-existent option %d picked", i+1, pick)
				}
			}
		}
		if len(r.Answer.Texts) > 0 {
			if len(r.Answer.Texts) != len(r.Answer.Picks) {
				return badRequest("%d answers of your own against %d questions in the answer",
					len(r.Answer.Texts), len(r.Answer.Picks))
			}
			for i, text := range r.Answer.Texts {
				if text == "" {
					continue
				}
				if strings.TrimSpace(text) == "" {
					return badRequest("your own answer to question %d is nothing but spaces", i+1)
				}
				if len([]rune(text)) > AskTextMax {
					return badRequest("your own answer to question %d is longer than %d characters", i+1, AskTextMax)
				}
				if err := safeText(text); err != nil {
					return err
				}
				if len(r.Answer.Picks[i]) > 0 {
					return badRequest(
						"question %d got both a pick and words of your own — in the dialog these are different items", i+1)
				}
			}
		}
		if len(r.Answer.Notes) > 0 {
			if len(r.Answer.Notes) != len(r.Answer.Picks) {
				return badRequest("%d notes against %d questions in the answer", len(r.Answer.Notes), len(r.Answer.Picks))
			}
			for i, note := range r.Answer.Notes {
				if note == "" {
					continue
				}
				if strings.TrimSpace(note) == "" {
					return badRequest("the note to question %d is nothing but spaces", i+1)
				}
				if len([]rune(note)) > AskTextMax {
					return badRequest("the note to question %d is longer than %d characters", i+1, AskTextMax)
				}
				if err := safeText(note); err != nil {
					return err
				}
				if len(r.Answer.Picks[i]) == 0 {
					return badRequest("the note to question %d stands beside nothing: no option is picked", i+1)
				}
			}
		}
	case r.Answer != nil:
		return badRequest("action %s takes no answers to questions", r.Kind)
	}
	switch {
	case r.Kind == SessionSend, r.Kind == SessionFile:
		if r.Kind == SessionSend && strings.TrimSpace(r.Text) == "" {
			return badRequest("a message to the session without text")
		}
		if len([]rune(r.Text)) > TextMax {
			return badRequest("the message is longer than %d characters", TextMax)
		}
		if err := safeText(r.Text); err != nil {
			return err
		}
		if name, refused := refusedCommand(r.Text); refused {
			return badRequest("/%s is not sent from the panel: %s", name, Refused[name])
		}
	case r.Text != "":
		return badRequest("action %s takes no message text", r.Kind)
	}
	if r.Kind == SessionFile {
		if len(r.Files) == 0 {
			return badRequest("a file with no content")
		}
		if len(r.Files) > FilesMax {
			return badRequest("%d files against a ceiling of %d at a time", len(r.Files), FilesMax)
		}
		total := 0
		for _, f := range r.Files {
			if len(f.Data) == 0 {
				return badRequest("file %q has no content", f.Name)
			}
			if len(f.Data) > FileMax {
				return badRequest("file %q is larger than %d MB", f.Name, FileMax>>20)
			}
			if err := safeFileName(f.Name); err != nil {
				return err
			}
			total += len(f.Data)
		}
		if total > FilesBytesMax {
			return badRequest("the files together weigh %d MB against a ceiling of %d MB at a time",
				total>>20, FilesBytesMax>>20)
		}
	} else if len(r.Files) > 0 {
		return badRequest("action %s takes no files", r.Kind)
	}
	if r.Kind == SessionCommand {
		if r.Command == nil || r.Command.Name == "" {
			return badRequest("a slash command without a name")
		}
		args, ok := Commands[r.Command.Name]
		if !ok {
			return badRequest("command %q is not in the allowed list: %s",
				r.Command.Name, strings.Join(commandNames(), ", "))
		}
		switch {
		case len(args) == 0 && r.Command.Arg != "":
			return badRequest("command /%s takes no argument", r.Command.Name)
		case r.Command.Name == "model" && ModelName(r.Command.Arg):
		case len(args) > 0 && !slices.Contains(args, r.Command.Arg):
			return badRequest("command /%s has no option %q; it has %s",
				r.Command.Name, r.Command.Arg, strings.Join(args, ", "))
		}
	} else if r.Command != nil {
		return badRequest("action %s takes no slash commands", r.Kind)
	}
	if r.Kind == SessionSet {
		if err := r.Setting.validate(); err != nil {
			return err
		}
	} else if r.Setting != nil {
		return badRequest("action %s changes no settings", r.Kind)
	}
	switch {
	case r.Kind == TaskStop, r.Kind == AgentStop:
		if r.Work == nil {
			return badRequest("action %s without the work to stop", r.Kind)
		}
		if r.Work.ID == "" {
			return badRequest("action %s without a work id", r.Kind)
		}
		if len(r.Work.ID) > idMax {
			return badRequest("the work id is longer than %d characters", idMax)
		}
		if !safeID(r.Work.ID) {
			return badRequest("the work id contains forbidden characters")
		}
		if r.Kind == AgentStop {
			if r.Work.Line != "" {
				return badRequest("%s has no line: the agent's name is its line on the screen", r.Kind)
			}
			break
		}
		if r.Work.Line == "" {
			return badRequest(
				"the panel does not know how this task is named on the session screen — there would be nothing to check the keypress against")
		}
		if len([]rune(r.Work.Line)) > workLineMax {
			return badRequest("the task line is longer than %d characters", workLineMax)
		}
		if strings.ContainsAny(r.Work.Line, "\n\t") {
			return badRequest("the task line spans several lines, while on the screen it is one line")
		}
		if err := safeText(r.Work.Line); err != nil {
			return err
		}
	case r.Work != nil:
		return badRequest("action %s stops no background work", r.Kind)
	}
	if r.Project != nil {
		if r.Kind != SessionOpen && r.Kind != SessionResume && r.Kind != SessionSwitch {
			return badRequest("action %s takes no project", r.Kind)
		}
		if err := r.Project.validate(); err != nil {
			return err
		}
	}
	if r.MessageID != "" {
		if r.Kind != SessionSend && r.Kind != SessionUnqueue {
			return badRequest("action %s names no message", r.Kind)
		}
		if !safeUUID(r.MessageID) {
			return badRequest("the message id does not look like a uuid")
		}
	} else if r.Kind == SessionUnqueue {
		return badRequest("action %s without the message to take back", r.Kind)
	}
	if r.Kind == SessionSwitch {
		if r.Switch == nil {
			return badRequest("action %s without where to move the session", r.Kind)
		}
		if r.Switch.To != SwitchConsole && r.Switch.To != SwitchStream {
			return badRequest("a session moves to %q or %q, not to %q", SwitchConsole, SwitchStream, r.Switch.To)
		}
		if r.Project == nil {
			return badRequest("action %s without the project: the session is started again with its launch parameters", r.Kind)
		}
	} else if r.Switch != nil {
		return badRequest("action %s moves no session", r.Kind)
	}
	if r.Kind == SessionResume && r.Resume == "" {
		return badRequest("resuming a session without a conversation id")
	}
	if r.Resume != "" {
		if r.Kind != SessionResume {
			return badRequest("action %s takes no conversation id", r.Kind)
		}
		if !safeUUID(r.Resume) {
			return badRequest("the conversation id does not look like a uuid")
		}
	}
	if r.Kind == ProjectCreate {
		if !strings.HasPrefix(r.Target, "/") {
			return badRequest("the project path %q is not absolute", r.Target)
		}
		return safePath(r.Target)
	}
	return safeTarget(r.Target)
}

func (p Project) validate() error {
	if p.Path == "" {
		return badRequest("a project without a path")
	}
	if len(p.Path) > pathMax {
		return badRequest("the project path is longer than %d characters", pathMax)
	}
	if !strings.HasPrefix(p.Path, "/") {
		return badRequest("the project path %q is not absolute", p.Path)
	}
	if err := safePath(p.Path); err != nil {
		return err
	}
	if p.Session == "" {
		return badRequest("a project without a session name")
	}
	if len(p.Session) > targetMax {
		return badRequest("the session name is longer than %d characters", targetMax)
	}
	if err := safeTarget(p.Session); err != nil {
		return err
	}
	if strings.Contains(p.Session, "/") {
		return badRequest("the session name %q contains /", p.Session)
	}
	if len(p.Launch) > 0 {
		if len(p.Launch) > launchMax {
			return badRequest("the launch parameters are longer than %d bytes", launchMax)
		}
		var obj map[string]any
		if err := json.Unmarshal(p.Launch, &obj); err != nil {
			return badRequest("the launch parameters are not a JSON object: %v", err)
		}
	}
	if p.ClaudeBin != "" {
		if len(p.ClaudeBin) > pathMax {
			return badRequest("the path to claude is longer than %d characters", pathMax)
		}
		if !strings.HasPrefix(p.ClaudeBin, "/") {
			return badRequest("the path to claude %q is not absolute", p.ClaudeBin)
		}
		if err := safePath(p.ClaudeBin); err != nil {
			return err
		}
	}
	if p.ConfigDir != "" {
		if len(p.ConfigDir) > pathMax {
			return badRequest("the profile config directory is longer than %d characters", pathMax)
		}
		if !strings.HasPrefix(p.ConfigDir, "/") {
			return badRequest("the profile config directory %q is not absolute", p.ConfigDir)
		}
		if err := safePath(p.ConfigDir); err != nil {
			return err
		}
	}
	return nil
}
