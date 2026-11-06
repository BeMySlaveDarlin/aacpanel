package hostcfg

import (
	"os"
	"strings"
	"testing"
)

// heavyOf is memoryOf with the queue of heavy runs named nowhere either.
func heavyOf(t *testing.T, totalKB string) {
	t.Helper()
	memoryOf(t, totalKB)
	for _, name := range []string{HeavySlotsEnv, HeavyReserveEnv, HeavyMemEnv, HeavyCPUsEnv} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

// Unnamed, the queue keeps a fifth of the machine free and gives a run that
// names nothing a tenth; three runs go at once on four cores each.
func TestTheQueueIsTheMachinesUnlessTheDescriptionSaysOtherwise(t *testing.T) {
	heavyOf(t, "65176588")
	got, err := ReadHeavy()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Heavy{Slots: 3, CPUs: 4, Reserve: "12G", Mem: "6G"}); got != want {
		t.Errorf("a description that names nothing gives %+v, want %+v", got, want)
	}

	t.Setenv(HeavySlotsEnv, "2")
	t.Setenv(HeavyCPUsEnv, "0")
	t.Setenv(HeavyReserveEnv, "")
	t.Setenv(HeavyMemEnv, "8G")
	got, err = ReadHeavy()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Heavy{Slots: 2, CPUs: 0, Reserve: "", Mem: "8G"}); got != want {
		t.Errorf("a description that names everything gives %+v, want %+v", got, want)
	}
}

// A value the queue cannot take is named in the error, and the default
// stands in for it.
func TestAQueueValueThatDoesNotReadGivesWayToTheDefault(t *testing.T) {
	heavyOf(t, "65176588")
	t.Setenv(HeavySlotsEnv, "three")
	t.Setenv(HeavyCPUsEnv, "-1")
	t.Setenv(HeavyMemEnv, "8GB")
	got, err := ReadHeavy()
	if want := (Heavy{Slots: 3, CPUs: 4, Reserve: "12G", Mem: "6G"}); got != want {
		t.Errorf("values that do not read give %+v, want %+v", got, want)
	}
	for _, want := range []string{HeavySlotsEnv + "=three", HeavyCPUsEnv + "=-1", HeavyMemEnv + "=8GB",
		"no ceiling. The ceiling is 6G"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}

func TestSizesAreCountedAsSystemdCountsThem(t *testing.T) {
	for v, want := range map[string]uint64{"8G": 8 << 30, "768M": 768 << 20, "512K": 512 << 10, "1T": 1 << 40,
		"1073741824": 1 << 30, " 6G ": 6 << 30} {
		if got, ok := SizeBytes(v); !ok || got != want {
			t.Errorf("%q counts as %d (%v), want %d", v, got, ok, want)
		}
	}
	for _, v := range []string{"8GB", "1.5G", "G", "-1G", "", "99999999999T"} {
		if got, ok := SizeBytes(v); ok {
			t.Errorf("%q counts as %d", v, got)
		}
	}
}
