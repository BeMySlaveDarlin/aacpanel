package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/claudecfg"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/historymove"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
	"aacpanel/internal/schema"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/stream"
)

const projectRootsEnv = hostcfg.ProjectRootsEnv

func homeSessionName() string {
	return hostcfg.Load().HomeSession
}

type project struct {
	Name      string
	Path      string
	Session   string
	Alias     string
	RC        string
	Launch    []byte
	ClaudeBin string
	ConfigDir string
	FromMap   bool
	// Parent is the session that had this one opened, by its name; see
	// action.Project.
	Parent   string
	OpenedBy string
	Codex    *action.CodexLaunch
	// IntentKey selects the saved message at launch; see launcher.Spec.
	IntentKey string
	// Role is the role session_open gives the session; see action.Project.
	Role *action.OpenRole
}

// Open starts a session of a project, as New does, or goes on with a thread
// of codex from the archive, and names the session it brought up: claude comes
// up under the name of its project or the first free one after it, and a codex
// thread under the tail of its id.
func (e *Executor) Open(ctx context.Context, req action.Request) (string, string, error) {
	if req.Kind == action.SessionResume && req.Agent == action.ResumeCodex {
		return e.codexResume(ctx, req.Contour, req.Resume)
	}
	return e.sessionOpen(ctx, req.Target, req.Project)
}

func (e *Executor) sessionOpen(ctx context.Context, target string, want *action.Project) (string, string, error) {
	e.projectMoves.Lock()
	defer e.projectMoves.Unlock()
	p, err := chooseProject(target, want)
	if err != nil {
		return "", "", err
	}
	if err := historymove.Pending(registry.ConfigDirs(), p.Path); err != nil {
		return "", "", err
	}

	if c, warns := launcher.ParseCodex(p.Launch); c.Agent == schema.AgentCodex {
		if p.Role != nil {
			return "", "", fmt.Errorf("a role is kept for a session of claude, and %s starts codex", p.Name)
		}
		return e.codexOpen(ctx, p, c, warns)
	}
	if p.Codex != nil {
		return "", "", fmt.Errorf("Codex launch options need agent codex")
	}
	owner, err := e.openingSession(p.OpenedBy)
	if err != nil {
		return "", "", err
	}

	trusted, err := projectTrustedFor(p)
	if err != nil {
		if !os.IsNotExist(err) {
			trusted = true
		} else {
			trusted = false
		}
	}

	granted := false
	if !trusted {
		if !p.FromMap {
			if claudecfg.AsksForTrust(p.Path) {
				return "", "", fmt.Errorf(
					"%s has never been opened in Claude Code: it is a repository, and a session started here "+
						"would stop at the directory trust question, which cannot be answered from the phone. "+
						"The panel grants trust only to projects from the map — add this directory as a "+
						"project, or open it once at the keyboard",
					p.Path)
			}
		} else {
			ok, err := trustProjectFor(p)
			if err != nil {
				return "", "", fmt.Errorf("%s is not trusted, and granting trust failed: %w", p.Path, err)
			}
			granted = ok
		}
	}

	p.IntentKey = sessionintent.First
	rep, err := e.runLauncher(ctx, p, "")
	if err != nil {
		return "", "", err
	}
	detail := describeConsole(rep)
	if err := keepOwner(rep, owner); err != nil {
		return detail, rep.Session, err
	}
	if granted {
		detail += "; trust for directory " + p.Path + " was granted by the panel — claude itself never asked about it"
	}
	return detail, rep.Session, nil
}

// keepOwner records the conversation that asked for the session just
// started, by its claude process: the right to close it and the reports go to
// that conversation, not to a later session of the same name.
func keepOwner(rep launcher.Report, owner stream.SessionOwner) error {
	if owner.Conversation == "" {
		return nil
	}
	key, err := processOwnerKey(rep.Agent)
	if err == nil {
		err = stream.KeepOwner(key, owner)
	}
	if err != nil {
		return fmt.Errorf("session %s started, but its opening session was not recorded: %w", rep.Session, err)
	}
	return nil
}

func chooseProject(target string, want *action.Project) (project, error) {
	if want != nil {
		path, err := checkProjectDir(want.Path)
		if err != nil {
			return project{}, err
		}
		return project{
			Name: want.Session, Path: path, Session: want.Session, RC: want.Session,
			Launch: want.Launch, ClaudeBin: want.ClaudeBin, ConfigDir: want.ConfigDir,
			FromMap: true, Parent: want.Parent, OpenedBy: want.OpenedBy, Codex: want.Codex, Role: want.Role,
		}, nil
	}
	list, err := projects()
	if err != nil {
		return project{}, err
	}
	return findProject(list, target)
}

func checkProjectDir(path string) (string, error) {
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("project path %q is not absolute", path)
	}
	roots := projectRoots()
	if len(roots) == 0 {
		return "", fmt.Errorf("no project roots are set: neither %s nor the home directory", projectRootsEnv)
	}
	return dirInside(path, roots)
}

// dirInside checks that an absolute path is an existing directory whose real
// path, links resolved, is inside one of the roots.
func dirInside(path string, roots []string) (string, error) {
	clean := filepath.Clean(path)
	if !insideRoots(clean, roots) {
		return "", fmt.Errorf("path %s is outside the allowed roots (%s)", clean, strings.Join(roots, ", "))
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("there is no directory %s: %w", clean, err)
	}
	if !insideRoots(real, roots) {
		return "", fmt.Errorf("path %s is a link leading to %s — outside the allowed roots", clean, real)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("directory %s cannot be read: %w", clean, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory — there is nothing to open in it", clean)
	}
	return clean, nil
}

func projectRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	var out []string
	for _, root := range hostcfg.ProjectRoots(home) {
		if !strings.HasPrefix(root, "/") || filepath.Clean(root) == "/" {
			continue
		}
		out = append(out, filepath.Clean(root))
	}
	return out
}

func insideRoots(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (e *Executor) sessionResume(ctx context.Context, target, resume string, want *action.Project) (string, error) {
	e.projectMoves.Lock()
	defer e.projectMoves.Unlock()
	p, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}
	if err := historymove.Pending(registry.ConfigDirs(), p.Path); err != nil {
		return "", err
	}
	owner, err := e.openingSession(p.OpenedBy)
	if err != nil {
		return "", err
	}

	if p.FromMap {
		if _, err := trustProjectFor(p); err != nil {
			return "", fmt.Errorf("granting trust for the resumed project: %w", err)
		}
	}
	p.IntentKey = sessionintent.First
	rep, err := e.runLauncher(ctx, p, resume)
	if err != nil {
		return "", err
	}
	detail := describeConsole(rep) + ", resumed conversation " + resume
	return detail, keepOwner(rep, owner)
}

func projects() ([]project, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	return []project{homeProject(home, homeSessionName())}, nil
}

func homeProject(home, name string) project {
	return project{
		Name:    "the host's main session",
		Path:    home,
		Session: name,
		Alias:   filepath.Base(home),
		RC:      name,
	}
}

func findProject(list []project, target string) (project, error) {
	for _, p := range list {
		if p.Session == target || (p.Alias != "" && p.Alias == target) {
			return p, nil
		}
	}
	if base, ok := withoutOrdinal(target); ok {
		for _, p := range list {
			if p.Session == base || (p.Alias != "" && p.Alias == base) {
				return p, nil
			}
		}
	}
	known := make([]string, 0, len(list))
	for _, p := range list {
		known = append(known, p.Session)
	}
	return project{}, fmt.Errorf("no project %q among the known ones: %s", target, strings.Join(known, ", "))
}

var ordinalRE = regexp.MustCompile(`^(.+)-\d+$`)

func withoutOrdinal(name string) (string, bool) {
	m := ordinalRE.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}
