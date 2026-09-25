package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"aacpanel/internal/chat"
	"aacpanel/internal/repo"
)

// The viewer asks for a project by its path on the host, and the path has to
// be one the map knows: the agent checks its own boundaries, and this checks
// that the panel was asked about a project of its own rather than about any
// directory a request cares to name.
func (s *Server) repoProject(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		http.Error(w, "the project is not named", http.StatusBadRequest)
		return "", "", false
	}
	// The project's own setting, when the map holds one. A panel running
	// without a database still reads repositories: what it loses is the
	// remembered base, and the agent then works the branch out itself.
	base := ""
	if s.db != nil {
		if found, err := s.db.ProjectBase(r.Context(), cwd); err == nil {
			base = found
		}
	}
	return cwd, base, true
}

func (s *Server) repoAsk(w http.ResponseWriter, r *http.Request, req chat.RepoReq) (*chat.RepoOut, bool) {
	if !s.chat.Available() {
		http.Error(w, "the repository is unreadable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return nil, false
	}
	out, err := s.chat.Repo(r.Context(), req)
	if err != nil {
		if errors.Is(err, chat.ErrNoRepo) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return nil, false
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return nil, false
	}
	return out, true
}

// apiRepoRefs answers with the branches and worktrees of a project.
func (s *Server) apiRepoRefs(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	out, ok := s.repoAsk(w, r, chat.RepoReq{Op: "refs", Cwd: cwd})
	if !ok {
		return
	}
	writeJSON(w, out)
}

// apiRepoChanges answers with every file the branch changed, in one list.
func (s *Server) apiRepoChanges(w http.ResponseWriter, r *http.Request) {
	cwd, base, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	// A base named in the query is the one-off choice made on the screen; the
	// project's own setting is what it falls back to.
	if asked := r.URL.Query().Get("base"); asked != "" {
		base = asked
	}
	out, ok := s.repoAsk(w, r, chat.RepoReq{Op: "changes", Cwd: cwd, Base: base})
	if !ok {
		return
	}
	writeJSON(w, out)
}

// apiRepoTree answers with the entries of one directory.
func (s *Server) apiRepoTree(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	out, ok := s.repoAsk(w, r, chat.RepoReq{Op: "tree", Cwd: cwd, Path: r.URL.Query().Get("path")})
	if !ok {
		return
	}
	writeJSON(w, out)
}

// apiRepoFind answers with the paths whose names carry what was typed.
func (s *Server) apiRepoFind(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	out, ok := s.repoAsk(w, r, chat.RepoReq{Op: "find", Cwd: cwd, Query: r.URL.Query().Get("q")})
	if !ok {
		return
	}
	writeJSON(w, out)
}

// RepoFile is a window of a file with its lines coloured.
type RepoFile struct {
	*chat.RepoOut
	Spans [][]repo.Span `json:"spans,omitempty"`
	Lexer string        `json:"lexer,omitempty"`
}

// apiRepoFile answers with a window of a file, coloured.
//
// The colouring happens here rather than in the browser: the panel would have
// to ship a second highlighter to do it there, and it already carries one for
// the feed. Here it costs a megabyte of the image and nothing on the wire —
// what travels is a run length and a class number per span.
func (s *Server) apiRepoFile(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	first, _ := strconv.Atoi(q.Get("first"))
	lines, _ := strconv.Atoi(q.Get("lines"))
	out, ok := s.repoAsk(w, r, chat.RepoReq{
		Op: "blob", Cwd: cwd, Path: q.Get("path"), Rev: q.Get("rev"),
		First: first, Lines: lines,
	})
	if !ok {
		return
	}
	file := RepoFile{RepoOut: out}
	if len(out.Lines) > 0 {
		file.Spans, file.Lexer = paintWindow(s.paint, out, func() (*chat.RepoOut, error) {
			return s.chat.Repo(r.Context(), chat.RepoReq{
				Op: "blob", Cwd: cwd, Path: out.Path, First: 1, Lines: wholeLines,
			})
		})
	}
	writeJSON(w, file)
}

// How many lines a read of a whole file asks for: the ceiling of one window
// at the agent. A file longer than that is past what is coloured anyway.
const wholeLines = 20000

// paintWindow colours a window of a file as part of the whole file. A lexer
// handed the middle of a file starts inside whatever was open there — a
// comment, a string — so the file is coloured whole, kept under the id of its
// content, and the window is cut out of that. The whole file is read only
// when it is not kept yet; a window that is the whole file is coloured as it
// came. A file past what is coloured at all stays text, whichever window of it
// is asked for.
func paintWindow(cache *repo.Cache, out *chat.RepoOut, whole func() (*chat.RepoOut, error)) ([][]repo.Span, string) {
	if out.Size > repo.MaxPaint {
		return nil, ""
	}
	window := strings.Join(out.Lines, "\n")
	if out.First <= 1 && !out.More {
		return cache.Painted(out.OID, out.Path, window)
	}
	lines, name, ok := cache.PaintedBy(out.OID, out.Path, func() (string, bool) {
		full, err := whole()
		// The file changed between the two reads, or runs past one read: the
		// colouring of what was read is not the colouring of this id.
		if err != nil || full == nil || full.Stale || full.More || full.OID != out.OID {
			return "", false
		}
		return strings.Join(full.Lines, "\n"), true
	})
	start := out.First - 1
	if !ok || start < 0 || start >= len(lines) {
		return nil, ""
	}
	// A file ending in empty lines is one line shorter to the lexer than to
	// the agent; the rows past the colouring are drawn as text.
	return lines[start:min(start+len(out.Lines), len(lines))], name
}

// RepoDiff is the diff of one file, cut into hunks and coloured.
type RepoDiff struct {
	Rev      string          `json:"rev,omitempty"`
	Stale    bool            `json:"stale,omitempty"`
	Path     string          `json:"path"`
	Base     string          `json:"base,omitempty"`
	BaseFrom string          `json:"baseFrom,omitempty"`
	Files    []repo.FileDiff `json:"files"`
}

// apiRepoDiff answers with the diff of one file, already cut into hunks.
func (s *Server) apiRepoDiff(w http.ResponseWriter, r *http.Request) {
	cwd, base, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if asked := q.Get("base"); asked != "" {
		base = asked
	}
	path := q.Get("path")
	out, ok := s.repoAsk(w, r, chat.RepoReq{
		Op: "diff", Cwd: cwd, Path: path, Base: base, Rev: q.Get("rev"), Layer: q.Get("layer"),
	})
	if !ok {
		return
	}

	answer := RepoDiff{Rev: out.Rev, Stale: out.Stale, Path: path, Base: out.Base, BaseFrom: out.BaseFrom}
	if out.Stale {
		writeJSON(w, answer)
		return
	}
	// The two layers are kept apart all the way to the screen: "already in a
	// commit" and "not yet" are different things to answer for in a review.
	for _, side := range []struct {
		text  string
		layer string
	}{
		{out.Committed, repo.LayerCommitted},
		{out.Worktree, repo.LayerWorktree},
	} {
		if strings.TrimSpace(side.text) == "" {
			continue
		}
		files, err := repo.Cut(side.text, side.layer)
		if err != nil {
			http.Error(w, "the diff was not read: "+err.Error(), http.StatusBadGateway)
			return
		}
		repo.PaintDiff(path, files)
		answer.Files = append(answer.Files, files...)
	}
	if answer.Files == nil {
		answer.Files = []repo.FileDiff{}
	}
	writeJSON(w, answer)
}

// apiRepoBlame answers with which commit last wrote each line of a window of
// a file: the window the viewer reads the file by.
func (s *Server) apiRepoBlame(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	first, _ := strconv.Atoi(q.Get("first"))
	lines, _ := strconv.Atoi(q.Get("lines"))
	out, ok := s.repoAsk(w, r, chat.RepoReq{
		Op: "blame", Cwd: cwd, Path: q.Get("path"), Rev: q.Get("rev"),
		First: first, Lines: lines,
	})
	if !ok {
		return
	}
	writeJSON(w, out)
}

// apiRepoCommit answers with who wrote a commit and in which conversation.
func (s *Server) apiRepoCommit(w http.ResponseWriter, r *http.Request) {
	cwd, _, ok := s.repoProject(w, r)
	if !ok {
		return
	}
	out, ok := s.repoAsk(w, r, chat.RepoReq{Op: "commit", Cwd: cwd, Hash: r.URL.Query().Get("hash")})
	if !ok {
		return
	}
	writeJSON(w, out)
}
