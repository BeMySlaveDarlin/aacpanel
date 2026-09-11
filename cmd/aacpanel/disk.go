package main

import (
	"context"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

type diskReply struct {
	State   string         `json:"state"`
	At      int64          `json:"at,omitempty"`
	Roots   []string       `json:"roots,omitempty"`
	Dirs    []host.DiskDir `json:"dirs,omitempty"`
	Missing []int          `json:"missing,omitempty"`
	Hidden  []string       `json:"hidden,omitempty"`
}

func (s *Server) diskReport(ctx context.Context, list []store.Profile) diskReply {
	var hidden map[string]bool
	if s.db != nil {
		var err error
		if hidden, err = s.db.HiddenDirs(ctx); err != nil {
			log.Printf("hidden directories: %v", err)
		}
	}
	if s.host == nil {
		return withHidden(diskReply{State: host.DiskUnknown}, hidden)
	}
	return withHidden(diskReport(s.host.Disk(), list), hidden)
}

func withHidden(out diskReply, hidden map[string]bool) diskReply {
	if len(hidden) == 0 {
		return out
	}
	kept := out.Dirs[:0:0]
	for _, d := range out.Dirs {
		if !hidden[d.Path] {
			kept = append(kept, d)
		}
	}
	out.Dirs = kept
	for path := range hidden {
		out.Hidden = append(out.Hidden, path)
	}
	sort.Strings(out.Hidden)
	return out
}

func diskReport(disk host.Disk, list []store.Profile) diskReply {
	if disk.State != host.DiskOK {
		return diskReply{State: host.DiskUnknown}
	}
	onMap := map[string]int{}
	for _, p := range list {
		for _, g := range p.Groups {
			for _, r := range g.Projects {
				onMap[filepath.Clean(r.Path)] = r.ID
			}
		}
	}
	seen := make(map[string]string, len(disk.Dirs))
	out := diskReply{State: host.DiskOK, At: disk.At, Roots: disk.Roots}
	for _, d := range disk.Dirs {
		seen[d.Path] = d.Kind
		if d.Kind != host.DirProject {
			continue
		}
		if _, ok := onMap[d.Path]; ok {
			continue
		}
		out.Dirs = append(out.Dirs, d)
	}
	for path, id := range onMap {
		if _, ok := seen[path]; ok {
			continue
		}
		if walked(filepath.Dir(path), disk, seen) {
			out.Missing = append(out.Missing, id)
		}
	}
	sort.Ints(out.Missing)
	return out
}

func walked(dir string, disk host.Disk, seen map[string]string) bool {
	for _, root := range disk.Roots {
		if dir == root {
			return true
		}
		if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			continue
		}
		if seen[dir] != host.DirFolder {
			return false
		}
		depth := strings.Count(strings.TrimPrefix(dir, root), string(filepath.Separator))
		return depth < disk.Depth
	}
	return false
}
