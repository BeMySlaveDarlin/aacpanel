package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The default budget is shares of the machine's memory: on the 62 GB of the
// panel's first machine it is the figures its owner settled on, and a small
// machine gets ceilings that neither vanish nor merge.
func TestTheDefaultBudgetIsSharesOfTheMachine(t *testing.T) {
	const kb = 1 << 10
	for _, c := range []struct {
		what    string
		total   uint64
		agents  Ceilings
		session Ceilings
	}{
		{"8 GB", 8 << 30, Ceilings{"3584M", "4G", "512M"}, Ceilings{"768M", "1280M", "256M"}},
		{"16 GB", 16 << 30, Ceilings{"7G", "9G", "1G"}, Ceilings{"1536M", "2816M", "256M"}},
		{"62 GB as the kernel tells it", 65176588 * kb, Ceilings{"28G", "34G", "4G"}, Ceilings{"6G", "10G", "1G"}},
		{"128 GB", 128 << 30, Ceilings{"58G", "70G", "8G"}, Ceilings{"13G", "21G", "2G"}},
	} {
		got := DefaultMemory(c.total)
		if got.Agents != c.agents {
			t.Errorf("%s: the slice of the agents is held to %+v, want %+v", c.what, got.Agents, c.agents)
		}
		if got.Session != c.session {
			t.Errorf("%s: a session is held to %+v, want %+v", c.what, got.Session, c.session)
		}
	}
}

// memoryOf has the machine hold total kB, and the description name nothing
// but what the test sets after.
func memoryOf(t *testing.T, totalKB string) {
	t.Helper()
	t.Setenv(PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
	for _, name := range []string{AgentsMemoryHighEnv, AgentsMemoryMaxEnv, AgentsSwapMaxEnv,
		SessionMemoryHighEnv, SessionMemoryMaxEnv, SessionSwapMaxEnv} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	was := meminfoFile
	meminfoFile = filepath.Join(t.TempDir(), "meminfo")
	t.Cleanup(func() { meminfoFile = was })
	body := "MemTotal:       " + totalKB + " kB\nMemFree:         1000000 kB\n"
	if err := os.WriteFile(meminfoFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheBudgetIsTheMachinesUnlessTheDescriptionSaysOtherwise(t *testing.T) {
	memoryOf(t, "65176588")
	got, err := ReadMemory()
	if err != nil {
		t.Fatal(err)
	}
	if want := DefaultMemory(65176588 << 10); got != want {
		t.Errorf("a description that names nothing gives %+v, want the machine's %+v", got, want)
	}

	t.Setenv(SessionMemoryMaxEnv, "12G")
	t.Setenv(SessionSwapMaxEnv, "")
	t.Setenv(AgentsSwapMaxEnv, "0")
	got, err = ReadMemory()
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Max != "12G" {
		t.Errorf("a named ceiling of a session reads as %q", got.Session.Max)
	}
	if got.Session.Swap != "" || got.Agents.Swap != "" {
		t.Errorf("a ceiling named empty or 0 is still there: session swap %q, slice swap %q",
			got.Session.Swap, got.Agents.Swap)
	}
	if got.Session.High != "6G" || got.Agents.Max != "34G" {
		t.Errorf("the ceilings left unnamed lost the machine's shares: %+v", got)
	}
}

func TestTheBudgetIsReadFromTheDescriptionFile(t *testing.T) {
	memoryOf(t, "65176588")
	file := filepath.Join(t.TempDir(), "host.env")
	body := "# the description\n" + AgentsMemoryMaxEnv + "=30G\n" + SessionMemoryHighEnv + "=\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PathEnv, file)
	got, err := ReadMemory()
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.Max != "30G" || got.Session.High != "" || got.Session.Max != "10G" {
		t.Errorf("the description file gives %+v", got)
	}
}

// A ceiling systemd would refuse would stop every unit it is passed to, so it
// is named in the error and the machine's share stands in its place.
func TestACeilingSystemdWouldRefuseGivesWayToTheShare(t *testing.T) {
	memoryOf(t, "65176588")
	for _, bad := range []string{"28GB", "1.5G", "40%", "G", "-1G", "6 G"} {
		t.Setenv(SessionMemoryMaxEnv, bad)
		got, err := ReadMemory()
		if got.Session.Max != "10G" {
			t.Errorf("%q gives a session ceiling of %q, not the machine's share", bad, got.Session.Max)
		}
		if err == nil || !strings.Contains(err.Error(), SessionMemoryMaxEnv+"="+bad) ||
			!strings.Contains(err.Error(), "10G") {
			t.Errorf("%q is not explained: %v", bad, err)
		}
	}
	for _, good := range []string{"6G", "768M", "1073741824", "512K", "1T"} {
		t.Setenv(SessionMemoryMaxEnv, good)
		if got, err := ReadMemory(); err != nil || got.Session.Max != good {
			t.Errorf("%q reads as %q, %v", good, got.Session.Max, err)
		}
	}
}

// A machine whose memory is not known has no shares to give: what the
// description does not name is no ceiling, and the error says so.
func TestAnUnknownMachineHasNoShares(t *testing.T) {
	memoryOf(t, "65176588")
	meminfoFile = filepath.Join(t.TempDir(), "absent")
	t.Setenv(SessionMemoryMaxEnv, "10G")
	got, err := ReadMemory()
	if err == nil || !strings.Contains(err.Error(), "memory of the machine is not known") {
		t.Errorf("the unknown machine is not named: %v", err)
	}
	if want := (Memory{Session: Ceilings{Max: "10G"}}); got != want {
		t.Errorf("an unknown machine gives %+v, want %+v", got, want)
	}
}
