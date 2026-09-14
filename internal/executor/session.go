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
	"aacpanel/internal/hostcfg"
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
}

func (e *Executor) sessionOpen(ctx context.Context, target string, want *action.Project) (string, error) {
	p, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}

	trusted, err := projectTrusted(p.Path)
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
				return "", fmt.Errorf(
					"%s has never been opened in Claude Code: it is a repository, and a session started here "+
						"would stop at the directory trust question, which cannot be answered from the phone. "+
						"The panel grants trust only to projects from the map — add this directory as a "+
						"project, or open it once at the keyboard",
					p.Path)
			}
		} else {
			ok, err := trustProject(p.Path)
			if err != nil {
				return "", fmt.Errorf("%s is not trusted, and granting trust failed: %w", p.Path, err)
			}
			granted = ok
		}
	}

	rep, err := e.runLauncher(ctx, p, "")
	if err != nil {
		return "", err
	}
	detail := describeConsole(rep)
	if granted {
		detail += "; trust for directory " + p.Path + " was granted by the panel — claude itself never asked about it"
	}
	return detail, nil
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
			FromMap: true,
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
	clean := filepath.Clean(path)
	roots := projectRoots()
	if len(roots) == 0 {
		return "", fmt.Errorf("no project roots are set: neither %s nor the home directory", projectRootsEnv)
	}
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
	p, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}

	rep, err := e.runLauncher(ctx, p, resume)
	if err != nil {
		return "", err
	}
	return describeConsole(rep) + ", resumed conversation " + resume, nil
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
