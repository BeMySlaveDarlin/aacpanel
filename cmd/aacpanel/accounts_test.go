package main

import (
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/store"
)

func TestSessionTokenOwnersFollowTheirOwnContour(t *testing.T) {
	profiles := []store.Profile{
		{ID: 11, Name: "Renamed owner", ConfigDir: "/srv/accounts/owner"},
		{ID: 22, Name: "Working", ConfigDir: "/srv/accounts/work"},
	}
	for _, tc := range []struct {
		name    string
		account string
		contour int
	}{
		{"own", `{"profile":"work","configDir":"/srv/accounts/work"}`, 22},
		{"borrowed", `{"profile":"owner","configDir":"/srv/accounts/owner/"}`, 11},
		{"unmapped", `{"profile":"external","configDir":"/srv/accounts/unmapped"}`, 0},
		{"unlisted", `{"profile":"external"}`, 0},
		{"unmarked", `null`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(`{"sessions":[{"session":"worker","profile":"work",` +
				`"configDir":"/srv/accounts/work","account":` + tc.account + `}]}`)
			got := sessionsByContour(payload, profiles)
			var snapshot struct {
				Sessions []struct {
					Contour int `json:"contour"`
					Account *struct {
						Profile string `json:"profile"`
						Contour int    `json:"contour"`
					} `json:"account"`
				} `json:"sessions"`
			}
			if err := json.Unmarshal(got, &snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Sessions) != 1 {
				t.Fatalf("the snapshot has %d sessions, want one", len(snapshot.Sessions))
			}
			row := snapshot.Sessions[0]
			if row.Contour != 22 {
				t.Errorf("the session moved into contour %d, want 22", row.Contour)
			}
			if tc.name == "unmarked" {
				if row.Account != nil {
					t.Errorf("an unmarked session acquired an account: %+v", row.Account)
				}
			} else if row.Account == nil || row.Account.Contour != tc.contour || row.Account.Profile == "" {
				t.Errorf("the token owner's account is %+v, want contour %d and its name", row.Account, tc.contour)
			}
			if strings.Contains(string(got), "configDir") || strings.Contains(string(got), "/srv/accounts") {
				t.Errorf("the snapshot exposes an account directory: %s", got)
			}
		})
	}
}
