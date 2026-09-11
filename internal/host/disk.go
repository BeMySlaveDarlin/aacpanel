package host

import (
	"encoding/json"
	"path/filepath"
)

// DiskOK and DiskUnknown are the states of the directory listing.
const (
	DiskOK      = "ok"
	DiskUnknown = "unknown"
)

// DirProject, DirFolder and DirLink are the kinds of a directory from the walk.
const (
	DirProject = "project"
	DirFolder  = "folder"
	DirLink    = "link"
)

// DiskDir is one directory from the walk.
type DiskDir struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Git    bool   `json:"git,omitempty"`
	Claude bool   `json:"claude,omitempty"`
}

// Disk is the snapshot of project directories: roots, walk depth and what was found.
type Disk struct {
	State string    `json:"state"`
	At    int64     `json:"at,omitempty"`
	Roots []string  `json:"roots,omitempty"`
	Depth int       `json:"depth,omitempty"`
	Dirs  []DiskDir `json:"dirs,omitempty"`
}

// Disk returns the project directories from the snapshot.
func (h *Reader) Disk() Disk {
	payload, err := h.Raw()
	if err != nil {
		return Disk{State: DiskUnknown}
	}
	var snapshot struct {
		Projects *struct {
			At    int64     `json:"at"`
			Roots []string  `json:"roots"`
			Depth int       `json:"depth"`
			Dirs  []DiskDir `json:"dirs"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil || snapshot.Projects == nil {
		return Disk{State: DiskUnknown}
	}
	p := snapshot.Projects
	out := Disk{State: DiskOK, At: p.At, Depth: p.Depth}
	for _, root := range p.Roots {
		if filepath.IsAbs(root) {
			out.Roots = append(out.Roots, filepath.Clean(root))
		}
	}
	if len(out.Roots) == 0 || out.Depth <= 0 {
		return Disk{State: DiskUnknown}
	}
	for _, d := range p.Dirs {
		if !filepath.IsAbs(d.Path) {
			continue
		}
		switch d.Kind {
		case DirProject, DirFolder, DirLink:
		default:
			continue
		}
		d.Path = filepath.Clean(d.Path)
		out.Dirs = append(out.Dirs, d)
	}
	return out
}
