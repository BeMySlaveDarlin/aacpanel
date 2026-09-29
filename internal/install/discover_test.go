package install

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/fs"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

// put lays paths out on the fake: a path ending in / is an empty
// directory, any other a file.
func put(m *fake, paths ...string) {
	for _, p := range paths {
		if dir, ok := strings.CutSuffix(p, "/"); ok {
			m.Stats[dir] = Stat{Mode: fs.ModeDir | 0o755, UID: m.Acct.UID}
			continue
		}
		m.Files[p] = ""
	}
}

// link makes path a symbolic link to a directory; paths put under it are
// what a walk that followed it would find. The mode carries ModeDir as well,
// or the fake would refuse to list the link and a walk that followed it
// would still find nothing.
func link(m *fake, path string) { m.Stats[path] = Stat{Mode: fs.ModeSymlink | fs.ModeDir | 0o777} }

func choices(cs []Choice) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Value+" | "+c.Source)
	}
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n%s\nwant:\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Only the terminals whose way of taking a command is known are offered, in
// the fixed order, each as the template the launcher reads.
func TestTerminalsOfferTheKnownOnesOnPath(t *testing.T) {
	m := healthy()
	m.Path["xterm"] = "/usr/bin/xterm"
	m.Path["gnome-terminal"] = "/usr/bin/gnome-terminal"
	m.Path["konsole"] = "/usr/bin/konsole"
	m.Path["foot"] = "/usr/bin/foot"
	equal(t, "terminals", choices(Terminals(m)), []string{
		"konsole | /usr/bin/konsole · the launcher calls it with its own flags: the directory and the tab title",
		"gnome-terminal -- | /usr/bin/gnome-terminal · the command goes after --",
		"xterm -e | /usr/bin/xterm · the command goes as arguments after -e",
	})
	if got := Terminals(healthy()); len(got) != 0 {
		t.Errorf("a machine without a terminal offers %v", got)
	}
}

func TestDisplays(t *testing.T) {
	for _, c := range []struct {
		name    string
		manager string // DISPLAY of the user manager; empty: systemctl --user fails
		sockets []string
		want    []string
	}{
		{
			// Over ssh there is no DISPLAY, while the desktop is running: the
			// sockets find it, in the order of their numbers.
			name:    "sockets only",
			sockets: []string{"X10", "X2", "X0", "not-a-display", "X1a", "X"},
			want: []string{
				":0 | socket in /tmp/.X11-unix",
				":2 | socket in /tmp/.X11-unix",
				":10 | socket in /tmp/.X11-unix",
			},
		},
		{
			// :10.0 and :10 are one display: the socket is not offered again.
			name:    "the user manager names a socket",
			manager: ":10.0",
			sockets: []string{"X0", "X10"},
			want: []string{
				":10.0 | from the user manager",
				":0 | socket in /tmp/.X11-unix",
			},
		},
		{
			// A forwarded display is another machine's :10, not this one's.
			name:    "a forwarded display",
			manager: "localhost:10.0",
			sockets: []string{"X10"},
			want: []string{
				"localhost:10.0 | from the user manager",
				":10 | socket in /tmp/.X11-unix",
			},
		},
		{name: "no graphics", want: []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			if c.manager != "" {
				m.Cmds[key("systemctl", "--user", "show-environment")] = ok("HOME=/home/u\nDISPLAY=" + c.manager + "\nLANG=C.UTF-8\n")
			}
			for _, s := range c.sockets {
				m.Stats["/tmp/.X11-unix/"+s] = Stat{Mode: fs.ModeSocket | 0o777}
			}
			equal(t, "displays", choices(Displays(m)), c.want)
		})
	}
}

func TestLocales(t *testing.T) {
	const all = "C\nC.utf8\nPOSIX\nen_US.utf8\nru_RU.utf8\nru_RU.UTF-8\nde_DE.iso88591\nsr_RS.utf8@latin\n"
	for _, c := range []struct {
		name, out, lang string
		want            []string
	}{
		{
			// locale -a prints ru_RU.utf8 and LANG takes ru_RU.UTF-8: one
			// locale, one line, written as LANG takes it.
			name: "LANG first", out: all, lang: "ru_RU.utf8",
			want: []string{
				"ru_RU.UTF-8 | $LANG, and in locale -a",
				"C.UTF-8 | always there: English messages, UTF-8 text",
				"en_US.UTF-8 | in locale -a",
				"sr_RS.UTF-8@latin | in locale -a",
			},
		},
		{
			name: "LANG the machine lacks", out: all, lang: "fr_FR.UTF-8",
			want: []string{
				"C.UTF-8 | always there: English messages, UTF-8 text",
				"en_US.UTF-8 | in locale -a",
				"ru_RU.UTF-8 | in locale -a",
				"sr_RS.UTF-8@latin | in locale -a",
			},
		},
		{
			name: "LANG is C.UTF-8", out: all, lang: "C.UTF-8",
			want: []string{
				"C.UTF-8 | $LANG, and in locale -a",
				"en_US.UTF-8 | in locale -a",
				"ru_RU.UTF-8 | in locale -a",
				"sr_RS.UTF-8@latin | in locale -a",
			},
		},
		{
			// C.UTF-8 is offered even where locale -a does not name it, and a
			// one-byte LANG is not offered at all.
			name: "no C.utf8 in locale -a", out: "en_US.utf8\nPOSIX\n", lang: "en_US",
			want: []string{
				"C.UTF-8 | always there: English messages, UTF-8 text",
				"en_US.UTF-8 | in locale -a",
			},
		},
		{
			name: "locale fails", lang: "C.UTF-8",
			want: []string{"C.UTF-8 | always there: English messages, UTF-8 text"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			if c.out != "" {
				m.Cmds[key("locale", "-a")] = ok(c.out)
			}
			m.Vars["LANG"] = c.lang
			equal(t, "locales", choices(Locales(m)), c.want)
		})
	}
}

// Only a leading tilde is the home directory: the answer goes to systemd and
// to a container, and neither expands one.
func TestExpand(t *testing.T) {
	for in, want := range map[string]string{
		"~":                   home,
		"~/bin/aacpanel-exec": home + "/bin/aacpanel-exec",
		"/srv/proj":           "/srv/proj",
		"/srv/~backup":        "/srv/~backup",
		"~other/x":            "~other/x",
	} {
		if got := Expand(in, home); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

// An account is a .claude or .claude-* directory holding something claude
// puts there itself; ~/.claude comes first.
func TestClaudeHomes(t *testing.T) {
	m := healthy()
	put(m,
		home+"/.claude-work/settings.json",
		home+"/.claude/.credentials.json",
		home+"/.claude-talked/sessions/",
		home+"/.claude-seen/statsig/x",
		home+"/.claude-empty/",                      // made for a future account
		home+"/.claude-notes/README.md",             // a similar name, nothing of claude
		home+"/.config/settings.json",               // settings.json of another program
		home+"/projects/shop/",                      // a marker's name, not an account's
		home+"/.claude.json",                        // a file
		home+"/.claude-profiles/work/settings.json", // an account one level down
	)
	link(m, home+"/.claude-disk")
	put(m, home+"/.claude-disk/.credentials.json")

	var got []string
	for _, a := range ClaudeHomes(m, home) {
		got = append(got, a.Dir+" signed in: "+map[bool]string{true: "yes", false: "no"}[a.SignedIn])
	}
	equal(t, "accounts", got, []string{
		home + "/.claude signed in: yes",
		home + "/.claude-disk signed in: yes",
		home + "/.claude-seen signed in: no",
		home + "/.claude-talked signed in: no",
		home + "/.claude-work signed in: no",
	})
	if got := ClaudeHomes(healthy(), home); len(got) != 0 {
		t.Errorf("a home without claude has accounts %v", got)
	}
}

// A directory opened under any account is known, with the newest time among
// the accounts.
func TestKnownSlugsReadEveryAccount(t *testing.T) {
	m := healthy()
	early := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	late := early.Add(48 * time.Hour)
	dir := func(path string, mod time.Time) { m.Stats[path] = Stat{Mode: fs.ModeDir | 0o700, Mod: mod} }
	dir(home+"/.claude/projects/-srv-shop", early)
	dir(home+"/.claude-work/projects/-srv-shop", late)
	dir(home+"/.claude/projects/-srv-lib", late)
	dir(home+"/.claude-work/projects/-srv-lib", early)
	dir(home+"/.claude-work/projects/-srv-api", early)
	put(m, home+"/.claude/projects/stray.json")

	got := KnownSlugs(m, []string{home + "/.claude", home + "/.claude-work", home + "/.claude-gone"})
	want := map[string]time.Time{"-srv-shop": late, "-srv-lib": late, "-srv-api": early}
	if len(got) != len(want) {
		t.Fatalf("known: %v", got)
	}
	for slug, at := range want {
		if !got[slug].Equal(at) {
			t.Errorf("%s: %v, want %v", slug, got[slug], at)
		}
	}
}

// The slug is the one claude names its transcript directories with: were
// they to differ, no directory would be known.
func TestSlugIsClaudes(t *testing.T) {
	for in, want := range map[string]string{
		"/srv/proj/born-shop.ru": "-srv-proj-born-shop-ru",
		"/home/u/My Site_2":      "-home-u-My-Site-2",
		"/srv/café":              "-srv-caf-",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScan(t *testing.T) {
	opened := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name  string
		paths []string
		links []string
		roots []string
		known map[string]time.Time
		want  []string // path, and "git" for a repository
	}{
		{
			// A marker makes a project; the walk does not go into one, so a
			// nested repository is part of it.
			name: "markers",
			paths: []string{
				"/srv/shop/.git/HEAD", "/srv/shop/web/.git/HEAD",
				"/srv/lib/CLAUDE.md",
				"/srv/infra/deep/.claude/settings.json",
				"/srv/tree/.git", // a worktree: .git is a file
				"/srv/notes/.claude",
				"/srv/empty/nothing/here/",
			},
			roots: []string{"/srv"},
			want:  []string{"/srv/infra/deep", "/srv/lib", "/srv/shop git", "/srv/tree git"},
		},
		{
			// Dependencies, build output and hidden directories hold other
			// people's repositories.
			name: "skipped directories",
			paths: []string{
				"/srv/node_modules/x/.git/HEAD", "/srv/vendor/y/CLAUDE.md", "/srv/dist/z/.git/HEAD",
				"/srv/build/a/.git/HEAD", "/srv/target/b/.git/HEAD", "/srv/__pycache__/c/.git/HEAD",
				"/srv/.cache/d/.git/HEAD", "/srv/app/.git/HEAD",
			},
			roots: []string{"/srv"},
			want:  []string{"/srv/app git"},
		},
		{
			name:  "three levels down",
			paths: []string{"/srv/a/b/c/.git/HEAD", "/srv/x/y/z/w/.git/HEAD"},
			roots: []string{"/srv"},
			want:  []string{"/srv/a/b/c git"},
		},
		{
			// A link leads anywhere, /etc included.
			name:  "links are not followed",
			paths: []string{"/srv/real/.git/HEAD", "/srv/link/.git/HEAD", "/srv/group/inner/.git/HEAD"},
			links: []string{"/srv/link", "/srv/group"},
			roots: []string{"/srv"},
			want:  []string{"/srv/real git"},
		},
		{
			// Someone works where claude was opened, markers or not.
			name:  "a directory claude opened",
			paths: []string{"/srv/notes/inner/", "/srv/plain/inner/"},
			roots: []string{"/srv"},
			known: map[string]time.Time{"-srv-notes": opened},
			want:  []string{"/srv/notes"},
		},
		{
			name:  "a root inside a root",
			paths: []string{"/srv/group/shop/.git/HEAD"},
			roots: []string{"/srv", "/srv/group", "/srv"},
			want:  []string{"/srv/group/shop git"},
		},
		{
			// Named as a root, a directory the first walk reached near its
			// bottom is walked the full three levels.
			name:  "a deep root inside a root",
			paths: []string{"/srv/a/b/c/d/.git/HEAD"},
			roots: []string{"/srv", "/srv/a/b"},
			want:  []string{"/srv/a/b/c/d git"},
		},
		{
			// The home directory is looked at one level deep, not walked.
			name: "the home directory",
			paths: []string{
				home + "/app/.git/HEAD", home + "/code/deep/.git/HEAD", home + "/.config/x/.git/HEAD",
			},
			roots: []string{"~"},
			want:  []string{home + "/app git"},
		},
		{
			// A root under the home directory is walked as any other, in
			// either order.
			name:  "a root under the home directory",
			paths: []string{home + "/app/.git/HEAD", home + "/code/deep/.git/HEAD"},
			roots: []string{"~/code", home},
			want:  []string{home + "/app git", home + "/code/deep git"},
		},
		{
			// Named as a root, a project is not listed as one; reached from
			// the home directory, it is.
			name:  "a project named as a root",
			paths: []string{home + "/app/.git/HEAD", home + "/app/sub/CLAUDE.md"},
			roots: []string{home + "/app", home},
			want:  []string{home + "/app git", home + "/app/sub"},
		},
		{
			// A walk that reaches the home directory from above does not walk
			// all of it either.
			name:  "the home directory from above",
			paths: []string{home + "/app/.git/HEAD", home + "/code/deep/.git/HEAD"},
			roots: []string{"/home"},
			want:  []string{home + "/app git"},
		},
		{
			name:  "relative and missing roots",
			paths: []string{"/srv/app/.git/HEAD"},
			roots: []string{"srv", "", "/nowhere"},
			want:  []string{},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			put(m, c.paths...)
			for _, l := range c.links {
				link(m, l)
			}
			got := Scan(m, c.roots, home, c.known)
			if got == nil {
				t.Fatal("nothing found came back nil")
			}
			var paths []string
			for _, p := range got {
				s := p.Path
				if p.Git {
					s += " git"
				}
				paths = append(paths, s)
			}
			equal(t, "projects", paths, append([]string{}, c.want...))
			for _, p := range got {
				if want := c.known[Slug(p.Path)]; !p.Opened.Equal(want) {
					t.Errorf("%s opened %v, want %v", p.Path, p.Opened, want)
				}
			}
		})
	}
}

// The directories claude opened come first, the newest first; the rest
// follow by path.
func TestScanPutsTheOpenedFirst(t *testing.T) {
	m := healthy()
	put(m, "/srv/a/.git/HEAD", "/srv/b/.git/HEAD", "/srv/c/.git/HEAD", "/srv/d/.git/HEAD")
	day := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	known := map[string]time.Time{"-srv-c": day, "-srv-d": day.Add(time.Hour)}
	var got []string
	for _, p := range Scan(m, []string{"/srv"}, home, known) {
		got = append(got, p.Path)
	}
	equal(t, "order", got, []string{"/srv/d", "/srv/c", "/srv/a", "/srv/b"})
}

func TestFindRoots(t *testing.T) {
	m := healthy()
	day := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	put(m,
		home+"/code/one/.git/HEAD", home+"/code/two/CLAUDE.md",
		clone+"/.git/HEAD", clone+"/sub/CLAUDE.md", // a project of the home directory, not a root
		home+"/notes/todo.txt",
		home+"/.cache/x/.git/HEAD",
		"/srv/www/a/.git/HEAD", "/srv/www/b/.git/HEAD", "/srv/www/c/.git/HEAD",
		"/srv/empty/",
		"/opt/tools/cli/.git/HEAD",
		"/opt/node_modules/x/.git/HEAD",
	)
	put(m, home+"/elsewhere/p/.git/HEAD")
	link(m, home+"/elsewhere")
	known := map[string]time.Time{"-opt-tools-cli": day, "-home-u-code-one": day, "-home-u-aacpanel": day}

	var got []string
	for _, r := range FindRoots(m, home, known) {
		got = append(got, r.Path+" "+strings.Repeat("p", r.Projects)+" "+strings.Repeat("o", r.Opened))
	}
	equal(t, "roots", got, []string{
		"/srv/www ppp ",
		home + "/code pp o",
		"/opt/tools p o",
	})
}

func TestAddresses(t *testing.T) {
	m := healthy()
	m.Cmds[key("ip", "-4", "-o", "addr")] = ok(strings.Join([]string{
		`1: lo    inet 127.0.0.1/8 scope host lo\       valid_lft forever preferred_lft forever`,
		`2: enp3s0    inet 192.168.1.20/24 brd 192.168.1.255 scope global dynamic enp3s0\       valid_lft 86000sec preferred_lft 86000sec`,
		`3: wlp4s0    inet 192.168.1.21/24 brd 192.168.1.255 scope global dynamic wlp4s0\       valid_lft 86000sec preferred_lft 86000sec`,
		`4: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\       valid_lft forever preferred_lft forever`,
		`5: br-0a1b2c3d4e5f    inet 172.18.0.1/16 brd 172.18.255.255 scope global br-0a1b2c3d4e5f\       valid_lft forever preferred_lft forever`,
		`6: veth1a2b3c@if5    inet 169.254.1.1/16 scope link veth1a2b3c\       valid_lft forever preferred_lft forever`,
		`7: tailscale0    inet 100.64.0.7/32 scope global tailscale0\       valid_lft forever preferred_lft forever`,
		`8: wg-home    inet 10.99.0.4/24 scope global wg-home\       valid_lft forever preferred_lft forever`,
		"",
	}, "\n"))
	lan, proxies := Addresses(m)
	equal(t, "lan", choices(lan), []string{
		"192.168.1.20 | on enp3s0",
		"192.168.1.21 | on wlp4s0",
	})
	equal(t, "proxies", choices(proxies), []string{
		"127.0.0.1 | the proxy runs on this machine",
		"172.17.0.1 | docker0: the proxy runs in a container",
		"10.99.0.4 | on wg-home: the proxy is on another machine behind WireGuard",
		"192.168.1.20 | on enp3s0",
		"192.168.1.21 | on wlp4s0",
	})

	// Without ip the proxy on this machine is still an answer.
	lan, proxies = Addresses(healthy())
	if len(lan) != 0 || len(proxies) != 1 || proxies[0].Value != "127.0.0.1" {
		t.Errorf("without ip: lan %v, proxies %v", lan, proxies)
	}
}

// pemCert is a self-signed certificate in PEM.
func pemCert(t *testing.T, cn string, dns ...string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dns,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestCertsArePairsNamedByTheCertificate(t *testing.T) {
	m := healthy()
	const dir = "/etc/panel-tls"
	keyBlock := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("k")}))
	m.Files[dir+"/panel.crt"] = pemCert(t, "ignored", "panel.example.org", "www.example.org")
	m.Files[dir+"/cn-only.crt"] = keyBlock + pemCert(t, "box.example.org")
	m.Files[dir+"/broken.crt"] = "not a certificate"
	m.Files[dir+"/nameless.crt"] = pemCert(t, "")
	m.Files[dir+"/panel.issuer.crt"] = pemCert(t, "issuer")
	m.Files[dir+"/lonely.crt"] = pemCert(t, "lonely.example.org")
	for _, k := range []string{"panel", "cn-only", "broken", "nameless"} {
		m.Files[dir+"/"+k+".key"] = "key"
	}
	put(m, dir+"/dir.crt/", dir+"/dir.key")

	var got []string
	for _, c := range Certs(m, []string{dir, "/nowhere", dir + "/"}) {
		got = append(got, c.Name+" "+c.Cert+" "+c.Key)
	}
	equal(t, "certificates", got, []string{
		"broken " + dir + "/broken.crt " + dir + "/broken.key",
		"box.example.org " + dir + "/cn-only.crt " + dir + "/cn-only.key",
		"nameless " + dir + "/nameless.crt " + dir + "/nameless.key",
		"panel.example.org " + dir + "/panel.crt " + dir + "/panel.key",
	})
}

func TestListeningPorts(t *testing.T) {
	m := healthy()
	m.Cmds[key("ss", "-Hltnp")] = ok(strings.Join([]string{
		`LISTEN 0 4096      0.0.0.0:5432  0.0.0.0:* users:(("postgres",pid=1234,fd=5))`,
		`LISTEN 0 4096         [::]:5432     [::]:* users:(("postgres",pid=1234,fd=6))`,
		`LISTEN 0 4096 127.0.0.53%lo:53   0.0.0.0:*`,
		`LISTEN 0 4096   127.0.0.54:53    0.0.0.0:*`,
		`LISTEN 0 4096    127.0.0.1:8776  0.0.0.0:*`,
		`LISTEN 0 4096    127.0.0.1:8777  0.0.0.0:*`,
		`LISTEN 0 4096 192.168.1.20:18443 0.0.0.0:*`,
		`LISTEN 0 128             *:22          *:*`,
		`LISTEN 0 4096 192.168.1.20:80    0.0.0.0:*`,
		`LISTEN 0 4096         [::]:80       [::]:*`,
		`LISTEN 0 4096 192.168.1.20:8080  0.0.0.0:*`,
		`LISTEN 0 30   127.0.0.1%lo:1659  0.0.0.0:* users:(("qbittorrent",pid=77,fd=27))`,
		`LISTEN 0 30 172.17.0.1%docker0:1659 0.0.0.0:* users:(("qbittorrent",pid=77,fd=42))`,
		`LISTEN 0 4096 [fe80::1%eth0]:9000 [::]:*`,
		`LISTEN 0 4096 [2001:db8::1]:9100 [::]:*`,
		`LISTEN 0 4096        [::1]:6379     [::]:* users:(("redis-server",pid=88,fd=6))`,
		`LISTEN 0 4096    127.0.0.1:40000 0.0.0.0:*`,
		`LISTEN 0 4096      0.0.0.0:3000  0.0.0.0:* users:(("my app,x=y",pid=99,fd=3))`,
		"",
	}, "\n"))
	equal(t, "ports", choices(ListeningPorts(m, []int{PortPanel, PortLocal, 18443})), []string{
		"port22=127.0.0.1:22 | something listens on 127.0.0.1:22",
		"port80=127.0.0.1:80 | something listens on 127.0.0.1:80",
		"qbittorrent=127.0.0.1:1659 | qbittorrent listens on 127.0.0.1:1659",
		"my-app-x-y=127.0.0.1:3000 | my-app-x-y listens on 127.0.0.1:3000",
		"postgres=127.0.0.1:5432 | postgres listens on 127.0.0.1:5432",
		"redis-server=127.0.0.1:6379 | redis-server listens on 127.0.0.1:6379",
		"port8080=192.168.1.20:8080 | something listens on 192.168.1.20:8080",
	})
	if got := ListeningPorts(healthy(), nil); len(got) != 0 {
		t.Errorf("nothing listens, and yet %v", got)
	}
}

// Discover wires every source in: the host name, the accounts behind Known,
// the projects under the roots, the ports without the panel's own, the
// address of the clone's git.
func TestDiscoverFillsEverything(t *testing.T) {
	m := healthy()
	day := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	m.Files["/proc/sys/kernel/hostname"] = "helios.example.org\n"
	m.Cmds[key("git", "-C", clone, "config", "user.email")] = ok("  u@example.org\n")
	m.Cmds[key("ss", "-Hltnp")] = ok("LISTEN 0 4096 0.0.0.0:18443 0.0.0.0:*\nLISTEN 0 4096 0.0.0.0:5432 0.0.0.0:*\n")
	m.Path["xterm"] = "/usr/bin/xterm"
	m.Stats["/tmp/.X11-unix/X0"] = Stat{Mode: fs.ModeSocket | 0o777}
	m.Cmds[key("locale", "-a")] = ok("C.utf8\nen_US.utf8\n")
	put(m, home+"/.claude/.credentials.json", "/srv/www/notes/")
	m.Stats[home+"/.claude/projects/-srv-www-notes"] = Stat{Mode: fs.ModeDir | 0o700, Mod: day}
	m.Files["/etc/panel-tls/panel.crt"] = pemCert(t, "panel.example.org")
	m.Files["/etc/panel-tls/panel.key"] = "key"
	m.Cmds[key("ip", "-4", "-o", "addr")] = ok("2: enp3s0    inet 192.168.1.20/24 scope global enp3s0\n")

	f := Discover(m, Facts{Account: m.Acct, Clone: clone, LANPort: 18443}, []string{"/etc/panel-tls"})
	if f.Host != "helios" || f.Email != "u@example.org" {
		t.Errorf("host %q, email %q", f.Host, f.Email)
	}
	if len(f.Accounts) != 1 || !f.Accounts[0].SignedIn || !f.Known["-srv-www-notes"].Equal(day) {
		t.Errorf("accounts %v, known %v", f.Accounts, f.Known)
	}
	if len(f.Roots) != 1 || f.Roots[0] != (Root{Path: "/srv/www", Projects: 1, Opened: 1}) {
		t.Errorf("roots %v", f.Roots)
	}
	if len(f.Ports) != 1 || f.Ports[0].Value != "port5432=127.0.0.1:5432" {
		t.Errorf("ports %v", f.Ports)
	}
	if len(f.Terminals) != 1 || len(f.Displays) != 1 || len(f.Locales) != 2 || len(f.LAN) != 1 ||
		len(f.Proxies) != 2 || len(f.Certs) != 1 {
		t.Errorf("terminals %v, displays %v, locales %v, lan %v, proxies %v, certs %v",
			f.Terminals, f.Displays, f.Locales, f.LAN, f.Proxies, f.Certs)
	}

	// Without the kernel's name, $HOSTNAME stands in.
	m = healthy()
	m.Vars["HOSTNAME"] = "box.lan"
	if got := Discover(m, Facts{Account: m.Acct}, nil); got.Host != "box" || got.Email != "" || got.Known == nil {
		t.Errorf("host %q, email %q, known %v", got.Host, got.Email, got.Known)
	}
}
