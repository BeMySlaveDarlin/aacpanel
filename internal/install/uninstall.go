package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// ---- uninstall ----

// Datum is a kind of the panel's data: uninstall deletes it only when the
// person chose it, and names it with the command that deletes it otherwise.
type Datum struct {
	ID, Label, Detail, Flag string
}

// Data are the data uninstall may delete, in the order it asks.
var Data = []Datum{
	{"db", "The database volume", "the map, the journal, the enrolled devices; the tailnet node's keys with it", "--purge-db"},
	{"state", "The state directory", "the collector's snapshot, the questions of sessions, the index of conversations", "--purge-state"},
	{"env", "The .env of the clone", "the secrets of the panel: without it a volume kept does not open", "--purge-env"},
	{"files", "Files from the phone", "what a phone sent to sessions", "--purge-files"},
	{"exec", "The executor's and the stream's state", "the guards of the map, the checklists, the answers to permissions", "--purge-exec"},
}

// PurgeAll is the flag that chooses every datum.
const PurgeAll = "--purge-data"

// Removal is uninstall: the lines of the manifest, a line a thing, taken
// back part by part in the order that keeps the machine working until the
// end — claude's wiring first, while the clone its hooks run from is
// there; the user's units; the executor's binary; the stack; the part as
// root, linger last of all that goes through systemd; the data chosen; the
// installer's cache; and its own directory last of everything.
type Removal struct {
	M       Machine
	Facts   Facts
	Place   *Place
	Entries []Entry
	// Cache is the installer's cache: Go, its modules, the builds. It goes
	// every time, since ./install.sh makes it again to build the installer.
	Cache string
	// Host is the name the person types to delete data.
	Host string

	purge []string
}

// NewRemoval reads the manifest into a removal. A thing written more than
// once counts once: by its first line, which says what it was before the
// installer, and for claude's settings by the last, whose sum says whether
// the file is still as the installer left it.
func NewRemoval(m Machine, f Facts, entries []Entry, cache string) *Removal {
	rm := &Removal{M: m, Facts: f, Place: &Place{Clone: f.Clone, User: f.Account.Name}, Cache: cache}
	at := map[string]int{}
	for _, e := range entries {
		k := e.Kind + "\x00" + e.Target
		i, seen := at[k]
		switch {
		case !seen:
			at[k] = len(rm.Entries)
			rm.Entries = append(rm.Entries, e)
		case e.Kind == string(JSON):
			rm.Entries[i] = e
		}
	}
	raw, _ := m.ReadFile(filepath.Join(f.StateDir, "host.env"))
	rm.Host = hostcfg.Parse(raw)[hostcfg.HostEnv]
	if rm.Host == "" {
		rm.Host = shortHost(m)
	}
	return rm
}

func (rm *Removal) of(kinds ...Kind) []Entry {
	var out []Entry
	for _, e := range rm.Entries {
		if slices.Contains(kinds, Kind(e.Kind)) {
			out = append(out, e)
		}
	}
	return out
}

func (rm *Removal) home() string          { return rm.Facts.Account.Home }
func (rm *Removal) state() string         { return rm.Facts.StateDir }
func (rm *Removal) short(p string) string { return rm.Facts.Short(p) }

// underState tells whether a path is the state directory or in it: the
// state goes whole, with the root part, or stays whole.
func (rm *Removal) underState(p string) bool {
	return p == rm.state() || strings.HasPrefix(p, rm.state()+"/")
}

// Purges tells whether a datum is chosen.
func (rm *Removal) Purges(id string) bool { return slices.Contains(rm.purge, id) }

// ErrNotTyped is a deletion of data the person did not confirm.
var ErrNotTyped = errors.New("the host name was not typed: the data stays")

// Choose takes the data to delete. Data goes only with the host name typed,
// or with --yes on a command line that named it: nothing else confirms a
// deletion that nothing takes back.
func (rm *Removal) Choose(ids []string, typed string, yes bool) error {
	var chosen []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if !slices.ContainsFunc(Data, func(d Datum) bool { return d.ID == id }) {
			return fmt.Errorf("no data is called %q", id)
		}
		chosen = append(chosen, id)
	}
	if len(chosen) > 0 && !yes && typed != rm.Host {
		return ErrNotTyped
	}
	rm.purge = chosen
	return nil
}

// RemoveQuestion is whether to remove the panel at all.
func (rm *Removal) RemoveQuestion() Question {
	return Question{ID: "remove", Prompt: "Remove the panel?", Form: One,
		Options: []Option{
			{Value: no, Label: "No", Detail: "Nothing is touched."},
			{Value: yes, Label: "Yes", Detail: "Everything in the plan above, in its order."},
		},
		Flag: "--yes", Values: []string{yes, no}, Default: no}
}

// DataQuestion is which data goes too; nothing is checked for the person.
func (rm *Removal) DataQuestion() Question {
	q := Question{ID: "purge", Prompt: "Delete data as well?", Form: Many,
		Note: "Space checks, Enter takes the list as it stands: nothing checked keeps it all.", Flag: PurgeAll}
	for _, d := range Data {
		q.Options = append(q.Options, Option{Value: d.ID, Label: d.Label, Detail: d.Detail + " (" + d.Flag + ")"})
	}
	return q
}

// HostQuestion asks for the host name before data goes.
func (rm *Removal) HostQuestion() Question {
	return Question{ID: "typed", Prompt: "Type the host name (" + rm.Host + ") to delete the data", Form: Line, Required: true,
		Flag: "--yes", Check: func(v string) error {
			if v != rm.Host {
				return fmt.Errorf("type %s, or Esc to keep the data", rm.Host)
			}
			return nil
		}}
}

// Plan is the removal as the plan frame reads it.
func (rm *Removal) Plan() []PlanRow {
	row := func(text string) PlanRow { return PlanRow{Text: "  · " + text} }
	var rows []PlanRow
	add := func(head string, lines ...string) {
		if len(lines) == 0 {
			return
		}
		rows = append(rows, PlanRow{Text: head, Head: true})
		for _, l := range lines {
			rows = append(rows, row(l))
		}
	}
	var claude []string
	for _, e := range rm.of(JSON) {
		how := "the panel taken out, the rest as you left it"
		if !metaHas(e.Meta, Adopted) && metaValue(e.Meta, "sha") != "" {
			how = "back as it was, if untouched since"
		}
		claude = append(claude, rm.short(e.Target)+": "+how)
	}
	for _, e := range rm.of(MCP) {
		claude = append(claude, "claude mcp remove aacpanel in "+rm.short(e.Target))
	}
	add("Claude settings — first, while the hooks' clone is there", claude...)
	var units []string
	for _, e := range rm.of(UserUnit) {
		units = append(units, filepath.Base(e.Target)+": disabled and its file removed")
	}
	add("Your units", units...)
	var files []string
	for _, e := range rm.of(File) {
		if metaHas(e.Meta, "data") || rm.underState(e.Target) || strings.HasPrefix(e.Target, rm.Facts.InstallDir+"/") {
			continue
		}
		files = append(files, rm.short(e.Target))
	}
	add("Files", files...)
	var docker []string
	if len(rm.of(Compose)) > 0 {
		docker = append(docker, "docker compose down --rmi local: the containers, their network, the image built here")
	}
	for _, e := range rm.of(TestDB) {
		if !metaHas(e.Meta, Adopted) {
			docker = append(docker, "the test database "+e.Target)
		}
	}
	for _, e := range rm.of(Image) {
		if metaHas(e.Meta, "pulled") {
			docker = append(docker, "the image "+e.Target+", pulled by the install")
		}
	}
	add("Docker", docker...)
	var root []string
	if len(rm.of(SysUnit)) > 0 || rm.hasSystemEnabled() {
		root = append(root, "aacpanel-agent@.service: disabled and removed")
	}
	if rm.lingerOff() {
		root = append(root, "linger for "+rm.Place.User+", which the installer turned on — last")
	}
	add("As root — one sudo", root...)
	add("Data — stays unless chosen below", rm.kept()...)
	add("Always", "the installer's cache "+rm.short(rm.Cache)+" (Go, modules, builds)",
		"the executor's cache "+rm.short(rm.execCache()), "its own directory "+rm.short(rm.Facts.InstallDir)+", last")
	var never []string
	for _, e := range rm.of(Pkg) {
		never = append(never, "the apt package "+e.Target)
	}
	for _, e := range rm.of(Group) {
		never = append(never, "the group "+e.Target)
	}
	for _, e := range rm.of(Linger) {
		if !rm.lingerOffFor(e) {
			never = append(never, "linger for "+e.Target+": it was on before the installer")
		}
	}
	never = append(never, "~/.cache/claude-tmp, trust marks in .claude.json, the tailnet node, the clone")
	add("Never touched", never...)
	return rows
}

// kept are the data of the manifest, as the plan names them.
func (rm *Removal) kept() []string {
	var out []string
	for _, e := range rm.of(Volume) {
		out = append(out, "the volume "+e.Target)
	}
	if rm.stateOwned() {
		out = append(out, rm.state())
	}
	for _, e := range rm.of(File) {
		if metaHas(e.Meta, "data") && !rm.underState(e.Target) {
			out = append(out, rm.short(e.Target))
		}
	}
	return append(out, rm.short(rm.filesDir()), rm.short(filepath.Join(rm.home(), ".local", "state", "aacpanel"))+" and aacpanel-stream")
}

func (rm *Removal) hasSystemEnabled() bool {
	for _, e := range rm.of(Enabled) {
		if strings.HasPrefix(e.Target, "system ") {
			return true
		}
	}
	return false
}

// lingerOffFor tells whether a line of linger is the installer's to turn
// off: turned on by it, and not found on.
func (rm *Removal) lingerOffFor(e Entry) bool {
	return metaHas(e.Meta, "by-installer") && !metaHas(e.Meta, Adopted)
}

func (rm *Removal) lingerOff() bool {
	return slices.ContainsFunc(rm.of(Linger), rm.lingerOffFor)
}

// stateOwned tells whether the manifest names the state directory: made by
// the root part, or found in place.
func (rm *Removal) stateOwned() bool {
	return slices.ContainsFunc(rm.of(Dir, File), func(e Entry) bool { return rm.underState(e.Target) })
}

func (rm *Removal) filesDir() string {
	if d := rm.M.Env("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "aacpanel-exec")
	}
	return filepath.Join(rm.home(), ".local", "share", "aacpanel-exec")
}

// execCache is the executor's cache: the directory its probe of the limits
// starts claude in.
func (rm *Removal) execCache() string {
	if d := rm.M.Env("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "aacpanel")
	}
	return filepath.Join(rm.home(), ".cache", "aacpanel")
}

// RootArgs are the arguments of the one call of root.sh remove, and whether
// there is anything for root to do at all.
func (rm *Removal) RootArgs() ([]string, bool) {
	args := []string{"remove", "--user", rm.Place.User, "--state", rm.state()}
	any := len(rm.of(SysUnit)) > 0 || rm.hasSystemEnabled()
	if rm.lingerOff() {
		args, any = append(args, "--linger"), true
	}
	if rm.Purges("state") && rm.stateOwned() {
		args, any = append(args, "--purge-state"), true
	}
	return args, any
}

// Steps are uninstall's parts in their order, U2 to U7; the report comes
// after them, and the installer's directory last of all.
func (rm *Removal) Steps() []*Step {
	return []*Step{
		{ID: "u-claude", Title: "Claude settings", Apply: rm.unwire},
		{ID: "u-units", Title: "Your units", Apply: rm.units},
		{ID: "u-files", Title: "The executor", Apply: rm.files},
		{ID: "u-docker", Title: "The stack", Apply: rm.docker},
		{ID: "u-root", Title: "Root part", Apply: rm.root},
		{ID: "u-data", Title: "Data and the installer's cache", Apply: rm.data},
	}
}

// LastStep is U9: the installer's own directory, the manifest with it.
func (rm *Removal) LastStep() *Step {
	return &Step{ID: "u-last", Title: "The installer's directory", Apply: func(r *Run) error {
		if err := os.RemoveAll(rm.Facts.InstallDir); err != nil {
			return err
		}
		r.Say(Pass, rm.short(rm.Facts.InstallDir)+" removed: nothing of the install is on record any more")
		return nil
	}}
}

// U2: claude's wiring, first, while the clone the hooks run from is there.
func (rm *Removal) unwire(r *Run) error {
	var dirs []string
	for _, e := range rm.of(JSON) {
		if err := undoJSON(r, e); err != nil {
			return err
		}
		dirs = append(dirs, filepath.Dir(e.Target))
		r.Say(Pass, rm.short(e.Target))
	}
	for _, e := range rm.of(MCP) {
		if err := undoMCP(r, e); err != nil {
			return fail("claude mcp remove aacpanel failed in "+rm.short(e.Target), err)
		}
		dirs = append(dirs, e.Target)
		r.Say(Pass, "no aacpanel server in "+rm.short(e.Target))
	}
	// What the status line and the executor wrote into an account.
	for _, dir := range dirs {
		for _, p := range []string{filepath.Join(dir, "rate-limits.json"), filepath.Join(dir, "session-models")} {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	if len(dirs) == 0 {
		r.Say(Note, "the manifest holds no claude settings")
	}
	return nil
}

// U3: the user's units, disabled and their files and links removed as
// files, so they go even with the user's manager away.
func (rm *Removal) units(r *Run) error {
	if exec := filepath.Join(rm.home(), "bin", "aacpanel-exec"); fileThere(exec) {
		out, _ := r.Exec(Cmd{Argv: []string{exec, "-sessions"}, Limit: 30 * time.Second})
		if live := strings.TrimSpace(out); live != "" {
			r.Say(Warn, "sessions on the stream end with the executor: "+strings.Join(strings.Fields(live), " "))
		}
	}
	for _, e := range rm.of(Enabled) {
		scope, unit, _ := strings.Cut(e.Target, " ")
		if scope != "user" {
			continue
		}
		if err := userctl(r, "disable", "--now", unit); err != nil {
			r.Say(Warn, "systemctl --user disable "+unit+" failed: its files go all the same")
		}
	}
	any := false
	for _, e := range rm.of(UserUnit) {
		unit := filepath.Base(e.Target)
		if err := userctl(r, "disable", "--now", unit); err != nil {
			r.Say(Warn, "systemctl --user disable "+unit+" failed: its files go all the same")
		}
		links, _ := filepath.Glob(filepath.Join(filepath.Dir(e.Target), "*.wants", unit))
		for _, l := range links {
			if st, err := os.Lstat(l); err == nil && st.Mode()&fs.ModeSymlink != 0 {
				if err := os.Remove(l); err != nil {
					return err
				}
			}
		}
		if err := undoFile(r, e); err != nil {
			return err
		}
		r.Say(Pass, unit+" disabled, its file removed")
		any = true
	}
	if any {
		_ = userctl(r, "daemon-reload")
	}
	return nil
}

func fileThere(p string) bool { _, err := os.Stat(p); return err == nil }

// U4: the executor's binary and the directories the installer made for it
// and for the units, each only if nothing else lives in it now.
func (rm *Removal) files(r *Run) error {
	for _, e := range rm.of(File) {
		if metaHas(e.Meta, "data") || rm.underState(e.Target) {
			continue
		}
		if err := undoFile(r, e); err != nil {
			return err
		}
		r.Say(Pass, rm.short(e.Target))
	}
	dirs := rm.of(Dir)
	for i := len(dirs) - 1; i >= 0; i-- {
		e := dirs[i]
		if e.Target == rm.Facts.InstallDir || metaHas(e.Meta, "cache") || rm.underState(e.Target) {
			continue
		}
		if err := undoDir(r, e); err != nil {
			return err
		}
	}
	return nil
}

// U5: the stack, its test database and the images the install brought.
func (rm *Removal) docker(r *Run) error {
	for _, e := range rm.of(Compose) {
		if err := undoCompose(r, e); err != nil {
			return fail("docker compose down failed", err)
		}
		r.Say(Pass, "compose project "+e.Target+" down, its local image removed")
	}
	for _, e := range rm.of(TestDB) {
		if err := undoTestDB(r, e); err != nil {
			return fail("the test database was not removed", err)
		}
	}
	for _, e := range rm.of(Image) {
		if err := undoImage(r, e); err != nil {
			return fail("the image "+e.Target+" was not removed", err)
		}
	}
	return nil
}

// U6: the part as root in one call: the collector's unit, linger — last of
// what goes through systemd, and only when the installer turned it on — and
// the state directory when it was chosen.
func (rm *Removal) root(r *Run) error {
	args, any := rm.RootArgs()
	if !any {
		r.Say(Note, "nothing of root's to take back")
		return nil
	}
	says := "Disables the collector and removes its unit"
	if slices.Contains(args, "--linger") {
		says += ", turns linger off"
	}
	if slices.Contains(args, "--purge-state") {
		says += ", deletes " + rm.state()
	}
	return r.AsRoot("Root command", says+". Nothing else runs as root.", args...)
}

// U7: the data chosen, and the installer's cache every time.
func (rm *Removal) data(r *Run) error {
	if rm.Purges("db") {
		for _, e := range rm.of(Volume) {
			_, err := r.Exec(Cmd{Argv: []string{"docker", "volume", "rm", e.Target}, Limit: time.Minute})
			if err != nil && !gone(err, "no such volume") {
				return fail("the volume "+e.Target+" was not deleted", err)
			}
			r.Say(Pass, "the volume "+e.Target+" deleted")
		}
	}
	if rm.Purges("env") {
		for _, e := range rm.of(File) {
			if !metaHas(e.Meta, "data") || rm.underState(e.Target) {
				continue
			}
			if err := purgeFile(e); err != nil {
				return err
			}
			r.Say(Pass, rm.short(e.Target)+" deleted")
		}
	}
	if rm.Purges("files") {
		if err := os.RemoveAll(rm.filesDir()); err != nil {
			return err
		}
		r.Say(Pass, rm.short(rm.filesDir())+" deleted")
	}
	if rm.Purges("exec") {
		for _, name := range []string{"aacpanel", "aacpanel-stream"} {
			p := filepath.Join(rm.home(), ".local", "state", name)
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			r.Say(Pass, rm.short(p)+" deleted")
		}
	}
	if err := removeCache(rm.Cache); err != nil {
		return err
	}
	r.Say(Pass, "the installer's cache "+rm.short(rm.Cache)+" removed")
	if fileThere(rm.execCache()) {
		if err := os.RemoveAll(rm.execCache()); err != nil {
			return err
		}
		r.Say(Pass, "the executor's cache "+rm.short(rm.execCache())+" removed")
	}
	return nil
}

// purgeFile deletes a file of data: back to the file that was there before
// the installer, or gone when there was none.
func purgeFile(e Entry) error {
	if orig := metaValue(e.Meta, "orig"); orig != "" {
		raw, err := os.ReadFile(orig)
		if err == nil {
			return writeFile(e.Target, raw, 0o600)
		}
	}
	return removeIfThere(e.Target)
}

// removeCache takes the installer's cache away whole. Go keeps its modules
// read-only, so the tree is made writable first.
func removeCache(dir string) error {
	if dir == "" {
		return nil
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// Left is U8: what uninstall leaves on the machine, with the command that
// deletes each, and what it never touches.
func (rm *Removal) Left() []string {
	var out []string
	if !rm.Purges("db") {
		for _, e := range rm.of(Volume) {
			out = append(out, "the volume "+e.Target+": docker volume rm "+e.Target)
		}
	}
	if !rm.Purges("state") && rm.stateOwned() {
		out = append(out, rm.state()+": sudo rm -r "+rm.state())
	}
	if !rm.Purges("env") {
		for _, e := range rm.of(File) {
			if metaHas(e.Meta, "data") && !rm.underState(e.Target) && fileThere(e.Target) {
				out = append(out, rm.short(e.Target)+": rm "+rm.short(e.Target))
			}
		}
	}
	if !rm.Purges("files") && fileThere(rm.filesDir()) {
		out = append(out, rm.short(rm.filesDir())+": rm -r "+rm.short(rm.filesDir()))
	}
	for _, e := range rm.of(TestDB) {
		if metaHas(e.Meta, Adopted) {
			out = append(out, "the test database "+e.Target+": docker rm -f -v "+e.Target)
		}
	}
	for _, e := range rm.of(Pkg) {
		out = append(out, "the apt package "+e.Target+": sudo apt remove "+e.Target)
	}
	for _, e := range rm.of(Group) {
		group, user, _ := strings.Cut(e.Target, " ")
		out = append(out, user+" in the group "+group+": sudo gpasswd -d "+user+" "+group)
	}
	for _, e := range rm.of(Linger) {
		if !rm.lingerOffFor(e) {
			out = append(out, "linger for "+e.Target+", on before the installer: sudo loginctl disable-linger "+e.Target)
		}
	}
	for _, e := range rm.of(TSNode) {
		out = append(out, "the node "+e.Target+" in the tailnet: the admin console, Machines")
	}
	// The build of the image fills docker's build cache, a gigabyte or so,
	// and docker keeps no mark of which build a piece of it came from.
	if slices.ContainsFunc(rm.of(Image), func(e Entry) bool { return metaHas(e.Meta, "local") }) {
		out = append(out, "docker's build cache, which the build of the image filled: docker builder prune — the cache of every build of this machine")
	}
	return append(out, "never touched: ~/.cache/claude-tmp, trust marks in .claude.json, the clone "+rm.short(rm.Place.Clone))
}

// Traces are what the machine holds of the panel without a manifest: the
// refusal of uninstall names them, since nothing on record says which are
// the installer's to take.
func Traces(in Inspection) []string {
	var out []string
	for _, t := range in.Traces {
		out = append(out, in.Short(t))
	}
	if len(out) == 0 {
		out = []string{"no trace of the panel either"}
	}
	return out
}

// NoManifest is the refusal of uninstall on a machine the installer has no
// record of.
func NoManifest(f Facts) error {
	return fmt.Errorf("stop: %s is not here, and it is the one account of what the installer put on this machine: "+
		"uninstall takes back nothing else. What the machine holds of the panel is listed below; INSTALL.md, "+
		"\"Removing the panel\", takes an install by hand away, or ./install.sh --adopt takes it over first",
		f.Short(filepath.Join(f.InstallDir, ManifestName)))
}

// CacheDir is the installer's cache, as install.sh names it.
func CacheDir(env func(string) string) string {
	if c := env("AACP_INSTALL_CACHE"); c != "" {
		return c
	}
	if base := env("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "aacpanel-install")
	}
	return filepath.Join(env("HOME"), ".cache", "aacpanel-install")
}

// UserBus gives a run the user's manager, as S5 does: an uninstall over ssh
// without a login of its own reaches it by its address.
func UserBus(r *Run, uid int) {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		return
	}
	rt := "/run/user/" + strconv.Itoa(uid)
	if r.Env == nil {
		r.Env = map[string]string{}
	}
	r.Env["XDG_RUNTIME_DIR"] = rt
	r.Env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + rt + "/bus"
}
