package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tasksOf(t *testing.T) {
	t.Helper()
	t.Setenv(PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
	for _, name := range []string{SessionTasksMaxEnv, AgentsTasksMaxEnv} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskCeilingsUseDefaultsUnlessNamed(t *testing.T) {
	tasksOf(t)
	got, err := ReadTasks()
	if want := (Tasks{Session: "8192", Agents: "24576"}); got != want || err != nil {
		t.Fatalf("an unnamed task budget is %+v, %v; want %+v", got, err, want)
	}
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"", ""}, {"0", ""}, {"infinity", "infinity"}, {"4096", "4096"}, {" 2048 ", "2048"},
	} {
		t.Run("value="+tc.raw, func(t *testing.T) {
			t.Setenv(SessionTasksMaxEnv, tc.raw)
			t.Setenv(AgentsTasksMaxEnv, tc.raw)
			got, err := ReadTasks()
			if got.Session != tc.want || got.Agents != tc.want || err != nil {
				t.Errorf("%q gives %+v, %v; want both ceilings %q", tc.raw, got, err, tc.want)
			}
		})
	}
}

func TestInvalidTaskCeilingsNameBothKeysAndUseDefaults(t *testing.T) {
	tasksOf(t)
	for _, bad := range []string{"many", "-1", "1.5", "8192K", "50%", "+1", "1 2", "18446744073709551616"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(SessionTasksMaxEnv, bad)
			t.Setenv(AgentsTasksMaxEnv, bad)
			got, err := ReadTasks()
			if want := (Tasks{Session: "8192", Agents: "24576"}); got != want {
				t.Errorf("%q gives %+v, want the defaults %+v", bad, got, want)
			}
			for _, key := range []string{SessionTasksMaxEnv, AgentsTasksMaxEnv} {
				if err == nil || !strings.Contains(err.Error(), key+"="+bad) {
					t.Errorf("%s=%s is not explained: %v", key, bad, err)
				}
			}
		})
	}
}

func TestTaskCeilingsAreReadFromTheDescription(t *testing.T) {
	tasksOf(t)
	file := filepath.Join(t.TempDir(), "host.env")
	if err := os.WriteFile(file, []byte(SessionTasksMaxEnv+"=4096\n"+AgentsTasksMaxEnv+"=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PathEnv, file)
	got, err := ReadTasks()
	if got.Session != "4096" || got.Agents != "" || err != nil {
		t.Errorf("the description gives %+v, %v", got, err)
	}
	t.Setenv(SessionTasksMaxEnv, "2048")
	if got, err := ReadTasks(); got.Session != "2048" || got.Agents != "" || err != nil {
		t.Errorf("the environment over the description gives %+v, %v", got, err)
	}
}
