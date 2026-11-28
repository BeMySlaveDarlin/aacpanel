package executor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/hostcfg"
)

func TestUnitsTakeTheTaskCeilingOrLeaveItToTheSlice(t *testing.T) {
	for _, ceiling := range []string{"8192", "", "infinity"} {
		t.Run("TasksMax="+ceiling, func(t *testing.T) {
			for what, args := range map[string][]string{
				"unit":    inSlice(hostcfg.Ceilings{}, ceiling),
				"session": launchArgs(nil, "/bin/true", launchFlag, "", hostcfg.Ceilings{}, ceiling),
				"daemon":  daemonArgs(nil, "/bin/true", "/home/u/.codex", t.TempDir(), "", ceiling),
			} {
				var props []string
				for _, arg := range args {
					if strings.HasPrefix(arg, "--property=TasksMax=") {
						props = append(props, arg)
					}
				}
				var want []string
				if ceiling != "" {
					want = []string{"--property=TasksMax=" + ceiling}
				}
				if !slices.Equal(props, want) {
					t.Errorf("%s has task properties %q, want %q", what, props, want)
				}
			}
			value := ceiling
			if value == "" {
				value = "infinity"
			}
			if args := sliceArgs(hostcfg.Ceilings{}, ceiling); !slices.Contains(args, "TasksMax="+value) {
				t.Errorf("the slice does not set TasksMax=%s: %q", value, args)
			}
		})
	}
}

func TestRunningUnitsTakeTheTaskCeilingDespiteARefusal(t *testing.T) {
	for _, ceiling := range []string{"8192", "", "infinity"} {
		t.Run("TasksMax="+ceiling, func(t *testing.T) {
			s := newStallScene(t, `if [ "$2" = show ]; then echo /;
elif [ "$4" = a-fail.service ]; then echo denied >&2; exit 1; fi`)
			before := cgroupRoot
			cgroupRoot = s.slice
			t.Cleanup(func() { cgroupRoot = before })
			for _, unit := range []string{"a-fail.service", "b-heavy.scope", "z-daemon.service", "other.slice/nested.service", "not-a-unit"} {
				if err := os.MkdirAll(filepath.Join(s.slice, unit), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(s.slice, "file.service"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			SetUpAgentTasks(context.Background(), ceiling)
			value := ceiling
			if value == "" {
				value = "infinity"
			}
			want := []string{"--user show --property=ControlGroup --value " + AgentsSlice}
			for _, unit := range []string{"a-fail.service", "b-heavy.scope", "z-daemon.service"} {
				want = append(want, "--user --runtime set-property "+unit+" TasksMax="+value)
			}
			if got := s.taken(); !slices.Equal(got, want) {
				t.Errorf("running units get %q, want %q", got, want)
			}
			if got := s.log.String(); !strings.Contains(got, "a-fail.service") || !strings.Contains(got, "denied") ||
				strings.Count(got, "was not set") != 1 {
				t.Errorf("the refused unit is not logged once with the reason: %q", got)
			}
		})
	}
}

func TestRunningTaskCeilingsHandleAnUnavailableSlice(t *testing.T) {
	for _, tc := range []struct {
		what   string
		answer string
		logged string
	}{
		{"no cgroup yet", "echo", ""},
		{"manager refused", "exit 1", "ceilings of running units were not set"},
		{"cgroup gone", "echo /absent", "units of " + AgentsSlice + " were not read"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			s := newStallScene(t, tc.answer)
			before := cgroupRoot
			cgroupRoot = s.slice
			t.Cleanup(func() { cgroupRoot = before })
			SetUpAgentTasks(context.Background(), "8192")
			if got := s.taken(); !slices.Equal(got, []string{"--user show --property=ControlGroup --value " + AgentsSlice}) {
				t.Errorf("an unavailable slice is set up with %q", got)
			}
			if tc.logged == "" {
				if s.log.Len() != 0 {
					t.Errorf("a slice with no units logs %q", s.log.String())
				}
			} else if !strings.Contains(s.log.String(), tc.logged) {
				t.Errorf("the unavailable slice is not explained: %q", s.log.String())
			}
		})
	}
}
