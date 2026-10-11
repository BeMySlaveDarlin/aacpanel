package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coresOf has the description name cores, as the variable does, on a machine
// whose cores are machine; an empty machine is one whose kernel lists none.
func coresOf(t *testing.T, value, machine string) (string, error) {
	t.Helper()
	t.Setenv(PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
	t.Setenv(CoresEnv, value)
	machineCores(t, machine)
	return Cores()
}

// machineCores has the kernel list cores as the machine's, or none at all.
func machineCores(t *testing.T, machine string) {
	t.Helper()
	was := presentFile
	presentFile = filepath.Join(t.TempDir(), "present")
	t.Cleanup(func() { presentFile = was })
	if machine != "" {
		if err := os.WriteFile(presentFile, []byte(machine+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoresAreTakenAsCPUAffinityTakesThem(t *testing.T) {
	for value, want := range map[string]string{
		"":              "",
		"   ":           "",
		"8-23":          "8-23",
		"8-11 16-23":    "8-11,16-23",
		"8-11,\t16-23,": "8-11,16-23",
		"0,2,4-7":       "0,2,4-7",
		"8-11,12-23":    "8-23",
		"3,1,2,2":       "1-3",
		"5":             "5",
		"7-7":           "7",
	} {
		got, err := coresOf(t, value, "0-23")
		if err != nil || got != want {
			t.Errorf("%q gives %q, %v; expected %q", value, got, err, want)
		}
	}
}

func TestCoresSystemdWouldRefuseAreRefusedHere(t *testing.T) {
	for _, value := range []string{
		"8-x", "23-8", "0,23-8", "numa", "8 - 23", "-3", "+3", "8-", "1.5", "%i", ",", "8-9-10", "0x8", "8192", "99999999999999999999",
	} {
		got, err := coresOf(t, value, "")
		if err == nil {
			t.Errorf("%q is taken as %q, and systemd does not read it so: it refuses it or drops a part", value, got)
			continue
		}
		if !strings.Contains(err.Error(), CoresEnv) {
			t.Errorf("%q is refused without naming the setting: %v", value, err)
		}
	}
}

func TestCoresTheMachineLacksAreRefused(t *testing.T) {
	_, err := coresOf(t, "8-31", "0-23")
	if err == nil {
		t.Fatal("cores past the machine's are taken: every unit would start on a core that is not there")
	}
	for _, want := range []string{"24-31", "0-23"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	if got, err := coresOf(t, "8-31", ""); err != nil || got != "8-31" {
		t.Errorf("where the kernel lists no cores the value is %q, %v: only systemd's form can be checked there", got, err)
	}
}

func TestCoresAreReadFromTheDescriptionFile(t *testing.T) {
	t.Setenv(CoresEnv, "")
	file := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(file, []byte("# the description\n"+CoresEnv+"=8-23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PathEnv, file)
	machineCores(t, "0-23")
	if got, err := Cores(); err != nil || got != "8-23" {
		t.Errorf("the description gives %q, %v", got, err)
	}
	t.Setenv(CoresEnv, "4-7")
	if got, err := Cores(); err != nil || got != "4-7" {
		t.Errorf("the variable gives %q, %v — it lost to the file", got, err)
	}
}
