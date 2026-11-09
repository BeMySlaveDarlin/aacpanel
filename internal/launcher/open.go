package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/stream"
)

// Console is an open claude session.
type Console struct {
	Session string `json:"session"`
	Agent   int    `json:"agent"`
	Konsole int    `json:"konsole"`
	Dir     string `json:"dir"`
	// Transport is how the session is kept: in tmux or on the stream.
	Transport string `json:"transport"`
	// Conversation is the id the session writes its transcript under.
	Conversation string `json:"conversation,omitempty"`
}

// Open reports what is open right now.
func Open() []Console {
	var out []Console
	for _, pid := range claudePIDs() {
		args := procArgs(pid)
		if oneShot(pid, args) {
			continue
		}
		name, ok := argValue(args, "-n", "--name")
		if !ok {
			continue
		}
		conversation, _ := argValue(args, "--session-id", "--resume", "-r")
		transport := TransportTmux
		if _, held := stream.Held(conversation, pid); held {
			transport = TransportStream
		}
		out = append(out, Console{
			Session:      name,
			Agent:        pid,
			Konsole:      konsoleOf(pid),
			Dir:          procCwd(pid),
			Transport:    transport,
			Conversation: conversation,
		})
	}
	slices.SortFunc(out, func(a, b Console) int { return strings.Compare(a.Session, b.Session) })
	return out
}

func procCwd(pid int) string {
	dir, err := os.Readlink(filepath.Join(procRoot(), strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return strings.TrimRight(dir, "/")
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

func checkResume(dir, resume string) error {
	if resume == "" {
		return nil
	}
	if !uuidRE.MatchString(resume) {
		return fmt.Errorf("%q does not look like a conversation id", resume)
	}

	judged := false
	for _, root := range projectRoots() {
		projects := filepath.Join(root, "projects")
		if info, err := os.Stat(projects); err != nil || !info.IsDir() {
			continue
		}
		judged = true
		for _, slug := range slugs(dir) {
			if _, err := os.Stat(filepath.Join(projects, slug, resume+".jsonl")); err == nil {
				return nil
			}
		}
	}
	if !judged {
		return nil
	}
	return fmt.Errorf("there is no conversation %s in directory %s: claude looks for a transcript only "+
		"under its own project, and a session started with someone else's dies right away", resume, dir)
}

// carryTranscript moves a conversation that goes on in another directory —
// its transcript and the folder of its subagents — from the project of the
// directory it ran in to the project of the one it goes on in, in the same
// contour. A transcript already there, or found nowhere, is left to
// checkResume.
func carryTranscript(from, to, resume string) error {
	if from == "" || resume == "" || !uuidRE.MatchString(resume) {
		return nil
	}
	for _, root := range projectRoots() {
		projects := filepath.Join(root, "projects")
		for _, slug := range slugs(to) {
			if _, err := os.Stat(filepath.Join(projects, slug, resume+".jsonl")); err == nil {
				return nil
			}
		}
	}
	for _, root := range projectRoots() {
		projects := filepath.Join(root, "projects")
		for _, slug := range slugs(from) {
			src := filepath.Join(projects, slug)
			if _, err := os.Stat(filepath.Join(src, resume+".jsonl")); err != nil {
				continue
			}
			dst := filepath.Join(projects, slugs(to)[0])
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return fmt.Errorf("the conversation %s was not carried to %s: %w", resume, to, err)
			}
			if info, err := os.Stat(filepath.Join(src, resume)); err == nil && info.IsDir() {
				if err := os.Rename(filepath.Join(src, resume), filepath.Join(dst, resume)); err != nil {
					return fmt.Errorf("the conversation %s was not carried to %s: %w", resume, to, err)
				}
			}
			if err := os.Rename(filepath.Join(src, resume+".jsonl"), filepath.Join(dst, resume+".jsonl")); err != nil {
				return fmt.Errorf("the conversation %s was not carried to %s: %w", resume, to, err)
			}
			return nil
		}
	}
	return nil
}

func slugs(dir string) []string {
	rep := strings.NewReplacer("/", "-", "_", "-", ".", "-")
	out := []string{rep.Replace(strings.TrimRight(dir, "/"))}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		if slug := rep.Replace(strings.TrimRight(real, "/")); slug != out[0] {
			out = append(out, slug)
		}
	}
	return out
}

func projectRoots() []string {
	return registry.ConfigDirs()
}

const configEnv = registry.HomeEnv

const registryEnv = registry.RegistryEnv
