package executor

import (
	"context"
	"encoding/json"

	"aacpanel/internal/action"
)

// Status answers what a session says about itself: the version of claude its
// file names, and on the stream the account claude named at the handshake. A
// terminal has no handshake to read, and its account stays unsaid.
func (e *Executor) Status(ctx context.Context, target string) (*action.Status, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.Status{Transport: action.SwitchConsole, Version: s.Version}, nil
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return nil, err
	}
	return &action.Status{Transport: action.SwitchStream, Version: s.Version, Account: initAccount(st.Init)}, nil
}

// initAccount reads the account out of claude's answer to the handshake.
func initAccount(init json.RawMessage) *action.Account {
	var body struct {
		Account struct {
			Email            string `json:"email"`
			Organization     string `json:"organization"`
			SubscriptionType string `json:"subscriptionType"`
			APIProvider      string `json:"apiProvider"`
		} `json:"account"`
	}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil {
		return nil
	}
	a := body.Account
	if a.Email == "" && a.Organization == "" && a.SubscriptionType == "" && a.APIProvider == "" {
		return nil
	}
	return &action.Account{Email: a.Email, Organization: a.Organization, Plan: a.SubscriptionType, Provider: a.APIProvider}
}
