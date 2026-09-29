package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The kinds the steps of the machine record. Data — the database volume,
// the .env with the password that opens it, the state directory — is not
// taken back by its undo: it names what is left and how to delete it, and
// deleting it is the person's choice at uninstall.
const (
	File     Kind = "file"     // a file; meta: created, orig=<backup>, replaced, data, sha=
	Pkg      Kind = "pkg"      // an apt package; never removed, only named
	Group    Kind = "group"    // "docker <user>"; never taken back, only named
	Linger   Kind = "linger"   // linger of the user, turned on by the installer
	SysUnit  Kind = "sysunit"  // the collector's unit in /etc/systemd/system
	UserUnit Kind = "userunit" // a unit in ~/.config/systemd/user
	Enabled  Kind = "enabled"  // "system <unit>" or "user <unit>"
	Compose  Kind = "compose"  // the compose project of the panel
	Volume   Kind = "volume"   // a docker volume of data
	Image    Kind = "image"    // an image built on this machine
	EnvKey   Kind = "envkey"   // a key of the .env the installer set; it goes with the .env
	DBRole   Kind = "dbrole"   // a role in the database; it goes with the volume
	TestDB   Kind = "testdb"   // the container of the test database
	Rev      Kind = "rev"      // the commit a run installed
	JSON     Kind = "json"     // claude's settings.json of an account; meta: created or orig=, and sha= of the write
	MCP      Kind = "mcp"      // the panel's MCP server of an account; meta: claude=, file=
	TSNode   Kind = "tsnode"   // the node in the tailnet; the admin console removes it
	Profile  Kind = "profile"  // an entry of the map; it lives in the database and goes with its volume
)

// The undos join the map in init: an undo that runs root.sh records what
// root.sh prints, Record looks the kind up in undos, and a map literal
// that led back to itself would not compile.
func init() {
	for k, undo := range map[Kind]func(*Run, Entry) error{
		File: undoFile, Pkg: undoPkg, Group: undoGroup, Linger: undoRoot, SysUnit: undoRoot,
		UserUnit: undoUserUnit, Enabled: undoEnabled, Compose: undoCompose, Volume: undoVolume,
		Image: undoImage, EnvKey: undoNothing, DBRole: undoNothing, TestDB: undoTestDB, Rev: undoNothing,
		JSON: undoJSON, MCP: undoMCP, TSNode: undoTSNode, Profile: undoNothing,
	} {
		undos[k] = undo
	}
}

// undoNothing is the undo of a line that stands for a part of something
// else: a key of the .env goes with the .env, a role with its volume.
func undoNothing(*Run, Entry) error { return nil }

func undoFile(r *Run, e Entry) error {
	switch {
	case metaHas(e.Meta, "data"):
		r.Say(Note, e.Target+" is left: it holds what the panel's data needs; delete it by hand if the data goes")
		return nil
	case metaHas(e.Meta, "replaced"):
		r.Say(Note, e.Target+" is left: it was there before the installer, which kept no copy")
		return nil
	}
	if orig := metaValue(e.Meta, "orig"); orig != "" && orig != "absent" {
		raw, err := os.ReadFile(orig)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		st, err := os.Stat(orig)
		if err != nil {
			return err
		}
		return writeFile(e.Target, raw, st.Mode().Perm())
	}
	return removeIfThere(e.Target)
}

func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func undoPkg(r *Run, e Entry) error {
	r.Say(Note, e.Target+" was installed with apt and stays: sudo apt remove "+e.Target)
	return nil
}

func undoGroup(r *Run, e Entry) error {
	group, user, _ := strings.Cut(e.Target, " ")
	r.Say(Note, user+" stays in the group "+group+": sudo gpasswd -d "+user+" "+group)
	return nil
}

// undoRoot takes back what root.sh made: the collector's unit, and linger
// when the installer turned it on.
func undoRoot(r *Run, e Entry) error {
	if r.Place == nil {
		return errors.New("this run does not know whose " + e.Kind + " " + e.Target + " it is")
	}
	args := []string{"remove", "--user", r.Place.User}
	if e.Kind == string(Linger) {
		if !metaHas(e.Meta, "by-installer") {
			return nil
		}
		args = append(args, "--linger")
	}
	return r.AsRoot("Root command", "Disables the collector and removes its unit.", args...)
}

func undoEnabled(r *Run, e Entry) error {
	scope, unit, _ := strings.Cut(e.Target, " ")
	if scope == "system" {
		return undoRoot(r, e)
	}
	return userctl(r, "disable", "--now", unit)
}

// userctl runs systemctl --user for an undo: a unit that is gone already
// is what the undo wanted.
func userctl(r *Run, args ...string) error {
	_, err := r.Exec(Cmd{Argv: append([]string{"systemctl", "--user"}, args...), Limit: time.Minute})
	if err != nil && gone(err, "does not exist", "not loaded", "not found") {
		return nil
	}
	return err
}

func undoUserUnit(r *Run, e Entry) error {
	if err := userctl(r, "disable", "--now", filepath.Base(e.Target)); err != nil {
		return err
	}
	if err := undoFile(r, e); err != nil {
		return err
	}
	return userctl(r, "daemon-reload")
}

func undoCompose(r *Run, e Entry) error {
	if r.Place == nil {
		return errors.New("this run does not know the clone of the compose project " + e.Target)
	}
	_, err := r.Exec(Cmd{Argv: []string{"docker", "compose", "down", "--rmi", "local"}, Dir: r.Place.Clone})
	return err
}

func undoVolume(r *Run, e Entry) error {
	r.Say(Note, "the volume "+e.Target+" is left with its data: docker volume rm "+e.Target+" deletes it")
	return nil
}

// undoImage removes an image the install built or pulled. One a container
// of something else runs from stays: it is not the panel's alone any more.
func undoImage(r *Run, e Entry) error {
	_, err := r.Exec(Cmd{Argv: []string{"docker", "image", "rm", e.Target}, Limit: time.Minute})
	switch {
	case err == nil, gone(err, "no such image"):
		return nil
	case gone(err, "conflict", "being used", "is using"):
		r.Say(Note, "the image "+e.Target+" is left: a container of something else uses it")
		return nil
	}
	return err
}

func undoTSNode(r *Run, e Entry) error {
	r.Say(Note, "the node "+e.Target+" stays in the tailnet: remove it in the admin console, Machines")
	return nil
}

func undoTestDB(r *Run, e Entry) error {
	if metaHas(e.Meta, Adopted) {
		r.Say(Note, "the test database "+e.Target+" is left: it was made by hand; docker rm -f -v "+e.Target+" deletes it")
		return nil
	}
	_, err := r.Exec(Cmd{Argv: []string{"docker", "rm", "-f", "-v", e.Target}})
	if err != nil && gone(err, "no such container") {
		return nil
	}
	return err
}

// gone tells whether a command failed only because what it was to remove is
// not there: the undo of a change that never happened.
func gone(err error, says ...string) bool {
	var fl *Failure
	if !errors.As(err, &fl) {
		return false
	}
	low := strings.ToLower(fl.Stderr)
	for _, s := range says {
		if strings.Contains(low, strings.ToLower(s)) {
			return true
		}
	}
	return false
}
