package install

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Var is a key the installer writes into host.env or the .env of the
// clone. An empty Value is written as it is: "AACP_TERMINAL=" means no
// windows, which a missing key does not. Comment goes above a key the file
// and its template lack.
type Var struct {
	Key, Value, Comment string
}

// Edit writes vars into a file of keys the way a person edits it. A key is
// set on its own line: the live one, or where the file has it commented
// out, as the templates keep the keys that are not needed everywhere. A key
// the file lacks goes at the end. A second live line of a key goes away:
// systemd and compose take the last, and an edit of the first would not
// apply. The keys of drop go back to how template has them — the line of a
// way in that is no longer taken returns to its commented form, and a key
// the template lacks leaves the file. Everything else stays byte for byte.
func Edit(file []byte, vars []Var, drop []string, template []byte) []byte {
	want := map[string]Var{}
	var order []string
	for _, v := range vars {
		if _, ok := want[v.Key]; !ok {
			order = append(order, v.Key)
		}
		want[v.Key] = v
	}
	gone := map[string]bool{}
	for _, k := range drop {
		if _, set := want[k]; !set {
			gone[k] = true
		}
	}
	lines := splitLines(file)
	target := map[string]int{}
	extra := map[int]bool{}
	for i, l := range lines {
		k, live := declares(l)
		if k == "" || (want[k].Key == "" && !gone[k]) {
			continue
		}
		j, seen := target[k]
		_, liveBefore := declares(lines[max(j, 0)])
		switch {
		case !seen:
			target[k] = i
		case live && !liveBefore:
			target[k] = i
		case live:
			extra[i] = true
		}
	}
	shapes := templateLines(template)
	var out []string
	for i, l := range lines {
		if extra[i] {
			continue
		}
		k, live := declares(l)
		if j, ok := target[k]; ok && j == i {
			if v, set := want[k]; set {
				out = append(out, v.Key+"="+v.Value)
				continue
			}
			if live {
				if shape, ok := shapes[k]; ok {
					out = append(out, shape)
				}
				continue
			}
		}
		out = append(out, l)
	}
	for _, k := range order {
		if _, ok := target[k]; ok {
			continue
		}
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		for _, c := range strings.Split(want[k].Comment, "\n") {
			if c != "" {
				out = append(out, "# "+c)
			}
		}
		out = append(out, k+"="+want[k].Value)
	}
	return []byte(strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n")
}

func splitLines(raw []byte) []string {
	text := strings.TrimSuffix(string(raw), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// declares tells which key a line sets, and whether the line is live. The
// templates comment a key out with the hash right before it, "#KEY=value";
// "# text = more" is prose and declares nothing.
func declares(line string) (key string, live bool) {
	s := strings.TrimSpace(line)
	live = !strings.HasPrefix(s, "#")
	s = strings.TrimPrefix(s, "#")
	k, _, ok := strings.Cut(s, "=")
	if !ok || !isKey(k) {
		return "", false
	}
	return k, live
}

func isKey(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, r := range k {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// templateLines are the lines a template sets each key on: the live one,
// else the first where it stands commented out.
func templateLines(template []byte) map[string]string {
	out := map[string]string{}
	liveSeen := map[string]bool{}
	for _, l := range splitLines(template) {
		k, live := declares(l)
		if k == "" || liveSeen[k] {
			continue
		}
		if _, seen := out[k]; seen && !live {
			continue
		}
		out[k] = strings.TrimSpace(l)
		liveSeen[k] = live
	}
	return out
}

// changed are the vars whose value in the parsed file differs, or whose key
// the file does not have: a key there with the same value is left alone,
// quotes and all.
func changed(have map[string]string, vars []Var) []Var {
	var out []Var
	for _, v := range vars {
		if cur, ok := have[v.Key]; !ok || cur != v.Value {
			out = append(out, v)
		}
	}
	return out
}

// live are the keys of drop the file still sets on a live line.
func live(file []byte, drop []string) []string {
	var out []string
	for _, l := range splitLines(file) {
		if k, on := declares(l); on && k != "" {
			for _, d := range drop {
				if d == k {
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// NewSecret is a random value as `openssl rand -hex` gives it: crypto/rand,
// and hex, which a DSN carries without escaping. There is no fallback: a
// secret made "somehow" is worse than none, which at least shows.
func NewSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("the random number generator is not there: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// writeFile puts data at path whole or not at all: a temporary file in
// the same directory, synced, renamed over the old one. A run that dies
// halfway leaves the old file, never half of the new.
func writeFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
