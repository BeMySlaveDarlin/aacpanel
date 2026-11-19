package main

import (
	"time"

	"aacpanel/internal/act"
)

type claimItem struct {
	ID      string     `json:"id"`
	Peer    string     `json:"peer,omitempty"`
	From    string     `json:"from,omitempty"`
	Expects string     `json:"expects"`
	Title   string     `json:"title"`
	At      time.Time  `json:"at"`
	Due     *time.Time `json:"due"`
	Rung    int        `json:"rung"`
	Overdue bool       `json:"overdue"`
}

type sessionClaims struct {
	Out []claimItem `json:"out"`
	In  []claimItem `json:"in"`
}

func claimsFor(rows []act.Record, session string) sessionClaims {
	out := sessionClaims{Out: []claimItem{}, In: []claimItem{}}
	for _, row := range rows {
		if row.Kind != "claim" || !row.Open() {
			continue
		}
		item := claimItem{ID: row.ID, Expects: row.Body.Expects, Title: row.Title, At: row.Created, Due: row.Due, Rung: row.Rung, Overdue: row.Overdue}
		if row.Session == session {
			item.Peer = row.Peer
			out.Out = append(out.Out, item)
		}
		if row.Peer == session {
			item.Peer = ""
			item.From = row.Session
			out.In = append(out.In, item)
		}
	}
	return out
}
