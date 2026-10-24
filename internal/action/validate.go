package action

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/role"
)

// ErrBadRequest means the request is not acceptable.
type ErrBadRequest struct{ msg string }

func (e ErrBadRequest) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return ErrBadRequest{msg: fmt.Sprintf(format, args...)}
}

// Validate checks a completed-work record without admitting its kind to the
// executor's protocol. A timestamp may be in the past: the work has started.
func (r WorkRecord) Validate(kind Kind, target string) error {
	if !RecordOnly(kind) {
		return badRequest("action %s is not a work record", kind)
	}
	if target == "" || len(target) > targetMax || !safeUUID(r.From) {
		return badRequest("a work record needs the calling session's name and conversation")
	}
	if err := safeTarget(target); err != nil {
		return err
	}
	if err := safeSessionName(r.Name); err != nil {
		return badRequest("the work record needs a valid observer or alarm name: %v", err)
	}
	if kind == ObserverStart {
		if strings.TrimSpace(r.Command) == "" || r.At != "" || r.Every != "" {
			return badRequest("an observer record needs its command and no alarm schedule")
		}
		if !filepath.IsAbs(r.Cwd) || len(r.Cwd) > pathMax {
			return badRequest("an observer record needs its absolute working directory")
		}
		return nil
	}
	if r.Command != "" || r.Cwd != "" {
		return badRequest("an alarm record carries no command or working directory")
	}
	if _, err := time.Parse(time.RFC3339, r.At); err != nil {
		return badRequest("an alarm record needs the first ring as an RFC 3339 moment")
	}
	if r.Every != "" {
		if span, err := time.ParseDuration(r.Every); err != nil || span <= 0 {
			return badRequest("an alarm record's period must be a positive duration")
		}
	}
	return nil
}

// Validate checks the request before it reaches anything that executes.
func (r Request) Validate() error {
	if r.AutoRestart && (r.Kind != SessionRestart || r.Resume != "" || !safeUUID(r.Conversation)) {
		return badRequest("an automatic restart needs its conversation and starts with an empty context")
	}
	if r.Anyway && !r.AutoRestart {
		return badRequest("only an automatic restart can end unfinished work after its reminder")
	}
	if r.Move != nil && r.Kind != ProjectMove {
		return badRequest("only a project move carries a move specification")
	}
	if r.Caller != "" && (r.Kind != SessionClose || !safeUUID(r.Caller)) {
		return badRequest("only a child close takes a caller conversation, which must be a uuid")
	}
	if r.Contour != "" && r.Ask != AskCodexModels && r.Agent != ResumeCodex {
		return badRequest("only the question of the codex models and a resume of codex name a contour")
	}
	if r.Discover && r.Ask != AskMcp {
		return badRequest("only the question of the MCP servers discovers them")
	}
	if r.Ask != "" {
		if r.Kind != "" || r.Resume != "" || r.Secret != nil || r.Agent != "" || r.Intents != nil {
			return badRequest("question %q performs no actions", r.Ask)
		}
		switch r.Ask {
		case AskKinds, AskTerms, AskSecrets, AskCodexModels:
			if r.Target != "" {
				return badRequest("question %q has no target", r.Ask)
			}
			if r.Contour != "" && !safeID(r.Contour) {
				return badRequest("the contour %q is not a name of one", r.Contour)
			}
		case AskGuards:
			return validateGuards(r)
		case AskPermission, AskWindow, AskModels, AskMcp, AskStatus, AskCommands, AskSide, AskSetup, AskProcesses, AskIntent, AskQuestion:
			if r.Target == "" {
				return badRequest("question %q without a session name", r.Ask)
			}
			if len(r.Target) > targetMax {
				return badRequest("the session name is longer than %d characters", targetMax)
			}
			if err := safeTarget(r.Target); err != nil {
				return err
			}
			if r.Ask == AskQuestion {
				return validateQuestion(r)
			}
			if r.Ask == AskSide {
				return validateSide(r)
			}
			if (r.Ask == AskSetup) != (r.Part != "") || (r.Part != "" && !SetupParts[r.Part]) {
				return badRequest("question %q names no screen the panel knows: %q", r.Ask, r.Part)
			}
			if r.Text != "" || len(r.History) > 0 {
				return badRequest("question %q carries no text", r.Ask)
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
	if r.Kind == ProjectCreate || r.Kind == ProjectMove {
		targetLimit = pathMax
	}
	if len(r.Target) > targetLimit {
		return badRequest("the target is longer than %d characters", targetLimit)
	}
	switch {
	case r.Kind == CodexUpdate || r.Kind == ClaudeUpdate:
		if r.Target != "host" {
			return badRequest("agent updates target the host")
		}
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
	case r.Kind == SessionSend, r.Kind == SessionFile, r.Kind == SessionLetter:
		if r.Kind != SessionFile && strings.TrimSpace(r.Text) == "" {
			return badRequest("a message to the session without text")
		}
		if len([]rune(r.Text)) > TextMax {
			return badRequest("the message is longer than %d characters", TextMax)
		}
		if err := safeText(r.Text); err != nil {
			return err
		}
		// A letter reaches the model inside its envelope, where a slash is
		// only a character: it runs no command.
		if name, refused := refusedCommand(r.Text); refused && r.Kind != SessionLetter {
			return badRequest("/%s is not sent from the panel: %s", name, Refused[name])
		}
	case r.Kind == SessionShell:
		if strings.TrimSpace(r.Text) == "" {
			return badRequest("a shell command without the command")
		}
		if len([]rune(r.Text)) > TextMax {
			return badRequest("the command is longer than %d characters", TextMax)
		}
		if err := safeText(r.Text); err != nil {
			return err
		}
	case r.Kind == SessionRestart && r.Text != "":
		if err := goingOnWith(r); err != nil {
			return err
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
			if err := f.checkPreview(); err != nil {
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
		if err := r.Command.validateCodex(); err != nil {
			return err
		}
		args, ok := Commands[r.Command.Name]
		if _, codex := CodexCommands[r.Command.Name]; codex && !ok {
			args, ok = nil, true
		}
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
	if r.Kind == SessionMcp {
		if err := r.Mcp.validate(); err != nil {
			return err
		}
	} else if r.Mcp != nil {
		return badRequest("action %s changes no MCP server", r.Kind)
	}
	switch {
	case r.Kind == SessionRename && CodexName(r.Target):
		if err := safeThreadName(r.Rename); err != nil {
			return err
		}
	case r.Kind == SessionRename:
		if err := safeSessionName(r.Rename); err != nil {
			return err
		}
		if r.Rename == r.Target {
			return badRequest("session %s is called so already", r.Target)
		}
	case r.Kind == TermRename:
		if err := safeTermName(r.Rename); err != nil {
			return err
		}
	case r.Rename != "":
		return badRequest("action %s renames no session", r.Kind)
	}
	if r.Place != "" && r.Kind != TermStart {
		return badRequest("action %s opens nothing in a place", r.Kind)
	}
	if r.Kind == SessionRemote {
		if r.Remote == nil {
			return badRequest("action %s without saying on or off", r.Kind)
		}
	} else if r.Remote != nil {
		return badRequest("action %s switches no remote control", r.Kind)
	}
	if r.Kind == SessionIntent {
		if err := validIntents(r.Intents); err != nil {
			return err
		}
	} else if r.Intents != nil {
		return badRequest("action %s sets no opening messages", r.Kind)
	}
	if r.Kind == SessionPin {
		if r.Pin == nil {
			return badRequest("action %s without saying pinned or unpinned", r.Kind)
		}
	} else if r.Pin != nil {
		return badRequest("action %s sets no pin", r.Kind)
	}
	if r.Kind == SessionRole {
		if r.Role == nil {
			return badRequest("action %s without the role: an empty one clears it", r.Kind)
		}
		if err := validRole(*r.Role); err != nil {
			return err
		}
	} else if r.Role != nil {
		return badRequest("action %s sets no role", r.Kind)
	}
	if r.Kind == SessionParent {
		if r.Parent == "" {
			return badRequest("action %s without the session to stand %s under", r.Kind, r.Target)
		}
		if err := ValidParent(r.Parent); err != nil {
			return err
		}
		if r.Parent == r.Target {
			return badRequest("session %s does not stand under itself", r.Target)
		}
	} else if r.Parent != "" {
		return badRequest("action %s stands no session under another", r.Kind)
	}
	switch {
	case r.Kind == ObserverStop, r.Kind == AlarmCancel:
		// The executor names an observer and an alarm by sixteen hex digits,
		// and nothing on a screen is their line.
		if r.Work == nil || !heldID(r.Work.ID) {
			return badRequest("action %s without the id the executor keeps it under", r.Kind)
		}
		if r.Work.Line != "" {
			return badRequest("action %s has no line: the id alone names it", r.Kind)
		}
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
		// A session on the stream stops a task by its id alone; the line is
		// what a console is checked against, and the executor asks for it there.
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
		if r.Kind != SessionOpen && r.Kind != SessionResume && r.Kind != SessionSwitch && r.Kind != SessionRestart {
			return badRequest("action %s takes no project", r.Kind)
		}
		if err := r.Project.validate(); err != nil {
			return err
		}
	}
	if r.MessageID != "" {
		if r.Kind != SessionSend && r.Kind != SessionUnqueue && r.Kind != SessionShell {
			return badRequest("action %s names no message", r.Kind)
		}
		if !safeUUID(r.MessageID) {
			return badRequest("the message id does not look like a uuid")
		}
	} else if r.Kind == SessionUnqueue {
		return badRequest("action %s without the message to take back", r.Kind)
	}
	if r.Asked != "" {
		if r.Kind != SessionSend {
			return badRequest("action %s answers no question", r.Kind)
		}
		if len(r.Asked) > idMax || !safeID(r.Asked) {
			return badRequest("the id of the question is not an id of codex's")
		}
	}
	if r.Use != "" {
		if r.Kind != SessionBackground {
			return badRequest("action %s moves no call", r.Kind)
		}
		if len(r.Use) > idMax {
			return badRequest("the call id is longer than %d characters", idMax)
		}
		if !safeID(r.Use) {
			return badRequest("the call id contains forbidden characters")
		}
	}
	if r.Kind == SessionLetter {
		if r.From == "" {
			return badRequest("a letter without the conversation it comes from: it would have no sender")
		}
		if !safeUUID(r.From) {
			return badRequest("the conversation a letter comes from does not look like a uuid")
		}
	} else if r.From != "" {
		return badRequest("action %s carries no letter", r.Kind)
	}
	if r.FromCodex != nil {
		if r.Kind != SessionLetter {
			return badRequest("action %s carries no letter", r.Kind)
		}
		for what, dir := range map[string]string{"the codex home": r.FromCodex.Home, "the directory": r.FromCodex.Dir} {
			if err := codexPlace(what, dir); err != nil {
				return err
			}
		}
	}
	if r.Kind == SessionSwitch {
		if r.Switch == nil {
			return badRequest("action %s without where to move the session", r.Kind)
		}
		if r.Switch.To != SwitchConsole && r.Switch.To != SwitchStream {
			return badRequest("a session moves to %q or %q, not to %q", SwitchConsole, SwitchStream, r.Switch.To)
		}
		if r.Switch.Window && r.Switch.To != SwitchConsole {
			return badRequest("a window opens to a session in tmux, not on the stream")
		}
		// A session on the stream goes to tmux with the project its
		// holder keeps; the other way the panel names the project.
		if r.Project == nil && r.Switch.To != SwitchConsole {
			return badRequest("action %s without the project: the session is started again with its launch parameters", r.Kind)
		}
	} else if r.Switch != nil {
		return badRequest("action %s moves no session", r.Kind)
	}
	if r.Kind == SessionResume && r.Resume == "" {
		return badRequest("resuming a session without a conversation id")
	}
	if r.Agent != "" {
		if r.Kind != SessionResume {
			return badRequest("action %s names no agent", r.Kind)
		}
		if r.Agent != ResumeCodex {
			return badRequest("a resume goes on with a conversation of claude or a thread of %s, not of %q", ResumeCodex, r.Agent)
		}
		if r.Contour == "" || !safeID(r.Contour) {
			return badRequest("a resume of a codex thread names the contour whose daemon keeps it, not %q", r.Contour)
		}
		if r.Project != nil {
			return badRequest("a resume of a codex thread takes no project: the daemon of its home knows where it ran")
		}
	}
	if r.Resume != "" {
		if r.Kind != SessionResume && r.Kind != SessionRestart {
			return badRequest("action %s takes no conversation id", r.Kind)
		}
		if !safeUUID(r.Resume) {
			return badRequest("the conversation id does not look like a uuid")
		}
	}
	if r.Kind == SecretPut || r.Kind == SecretDrop {
		if err := validateSecret(r); err != nil {
			return err
		}
	} else if r.Secret != nil {
		return badRequest("action %s saves no secret", r.Kind)
	}
	if r.Kind == SecretDrop {
		return nil
	}
	if r.Kind == ProjectMove {
		if r.Move == nil || r.Move.From == r.Move.To || !safeID(r.Move.ID) || r.Move.ID == "" || len(r.Move.ID) > idMax {
			return badRequest("a project move needs two different config directories")
		}
		for _, path := range []string{r.Target, r.Move.From, r.Move.To} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > pathMax {
				return badRequest("a project move needs clean absolute paths")
			}
			if err := safePath(path); err != nil {
				return err
			}
		}
		if r.Move.Finish != "" && r.Move.Finish != "commit" && r.Move.Finish != "rollback" {
			return badRequest("unknown project move finish %q", r.Move.Finish)
		}
		return nil
	}
	if r.Kind == ProjectCreate {
		if !strings.HasPrefix(r.Target, "/") {
			return badRequest("the project path %q is not absolute", r.Target)
		}
		return safePath(r.Target)
	}
	if TermKind(r.Kind) {
		return validateTerm(r)
	}
	return safeTarget(r.Target)
}

// goingOnWith keeps intent on manual restarts tied to a continued conversation.
// The service's context cycle can instead carry it into a fresh one.
func goingOnWith(r Request) error {
	if r.Resume == "" && !r.AutoRestart {
		return badRequest("a message to go on with is for a restart that goes on with the conversation; " +
			"one that starts anew opens with the map's message after a restart")
	}
	return RestartIntent(r.Text)
}

// RestartIntent checks words used as the first message, either on a continued
// conversation or after the session has saved its state for an automatic restart.
func RestartIntent(intent string) error {
	text := strings.TrimSpace(intent)
	if text == "" {
		return badRequest("the message to go on with is empty")
	}
	if len([]rune(intent)) > IntentMax {
		return badRequest("the message to go on with is longer than %d characters", IntentMax)
	}
	if strings.HasPrefix(text, "-") {
		return badRequest("the message to go on with starts with a dash, which claude in a terminal takes for an option")
	}
	if strings.HasPrefix(text, "/") {
		return badRequest("the message to go on with starts with /, which claude runs as a command rather than " +
			"reads as words")
	}
	return safeText(intent)
}

// ValidParent checks the name of the session another stands under. It goes
// into the environment of the child and back into the snapshot, so it is a
// session name like any other: one with a way out of a path or a character
// the names never carry is refused.
func ValidParent(name string) error {
	if len(name) > targetMax {
		return badRequest("the name of the parent session is longer than %d characters", targetMax)
	}
	if err := safeTarget(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return badRequest("the name of the parent session %q contains /", name)
	}
	return nil
}

// validRole checks a role as the session's role tool would keep it: no
// longer than a role is, and with no control characters but line breaks and
// tabs.
func validRole(text string) error {
	if _, err := role.Clean(text); err != nil {
		return badRequest("%v", err)
	}
	return safeText(text)
}

func (p Project) validate() error {
	if p.OpenedBy != "" && !safeUUID(p.OpenedBy) {
		return badRequest("the conversation opening a session must be a uuid")
	}
	if p.Role != nil {
		if err := validRole(p.Role.Text); err != nil {
			return err
		}
		if p.Role.Opener != "" {
			if err := ValidParent(p.Role.Opener); err != nil {
				return err
			}
		}
	}
	if p.Codex != nil {
		if err := p.Codex.Validate(); err != nil {
			return err
		}
	}
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
	if p.Parent != "" {
		if err := ValidParent(p.Parent); err != nil {
			return err
		}
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
