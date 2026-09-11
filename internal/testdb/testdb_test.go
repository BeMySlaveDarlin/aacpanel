package testdb

import (
	"strconv"
	"testing"
	"time"
)

func TestOrphanReadsTheRunMark(t *testing.T) {
	now := time.Now()
	old := now.Add(-3 * time.Hour).Unix()
	fresh := now.Add(-time.Minute).Unix()

	cases := []struct {
		name string
		db   string
		want bool
	}{
		{"a database of this run is left alone", prefix + "store_" + run, false},
		{"a fresh one of somebody else has a live owner", prefix + "store_" + strconv.FormatInt(fresh, 10) + "_777", false},
		{"an old one of somebody else has no owner left", prefix + "store_" + strconv.FormatInt(old, 10) + "_777", true},
		{"with no mark it is a trace of the previous scheme and would live forever", prefix + "store", true},
		{"a mark that does not parse counts as abandoned", prefix + "store_zzz_777", true},
		{"somebody else's prefix is none of our business", "aacpanel", false},
		{"the production database is none of our business", "aacpanel_prod", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := orphan(c.db, now); got != c.want {
				t.Errorf("orphan(%q) = %v, expected %v", c.db, got, c.want)
			}
		})
	}
}

func TestOrphanSparesOwnDatabaseOnALongRun(t *testing.T) {
	late := time.Now().Add(orphanAge + time.Hour)
	if orphan(prefix+"store_"+run, late) {
		t.Error("a database of this run was taken away on a run lasting longer than the abandonment age")
	}
	if !orphan(prefix+"store_"+strconv.FormatInt(time.Now().Unix(), 10)+"_777", late) {
		t.Error("a database of somebody else of the same age has to be taken away — otherwise the check means nothing")
	}
}

func TestOrphanSurvivesUnderscoreInPackageName(t *testing.T) {
	now := time.Now()
	old := strconv.FormatInt(now.Add(-3*time.Hour).Unix(), 10) + "_777"
	if !orphan(prefix+"cmd_aacpanel_"+old, now) {
		t.Error("an old database of a package with an underscore has to be taken away")
	}
	if orphan(prefix+"cmd_aacpanel_"+run, now) {
		t.Error("a database of this run for a package with an underscore must not be taken away")
	}
	fresh := strconv.FormatInt(now.Add(-time.Minute).Unix(), 10) + "_777"
	if orphan(prefix+"cmd_aacpanel_"+fresh, now) {
		t.Error("a fresh database of somebody else for a package with an underscore was taken away — a live run was wiped")
	}
}
