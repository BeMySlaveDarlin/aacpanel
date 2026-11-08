package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/taskreg"
)

func TestTaskAndHandoffCLICommandsKeepExplicitBodies(t *testing.T) {
	requests := []taskreg.Request{}
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/tasks/registry" {
			t.Errorf("registry route: %s %s", r.Method, r.URL.Path)
		}
		var in taskreg.Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		requests = append(requests, in)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	file := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(file, []byte("Complete markdown\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		handoff         bool
		args            []string
		operation, body string
	}{
		{false, []string{"switch", "12"}, "switch", ""},
		{false, []string{"next", "-project", "12", "-tag", "backend", "-q", "work"}, "list", ""},
		{false, []string{"create", "-project", "12", "-title", "Work", "-priority", "HIGH", "-tags", "backend, urgent", "-body-file", file, "-replace"}, "create", "Complete markdown\n"},
		{false, []string{"update", "-project", "12", "-number", "3", "-status", "deferred", "-blocked-by", "1,2", "-external", "https://issues.example/LMS-42", "-stdin", "-section", "Plan"}, "update", "From stdin"},
		{false, []string{"close", "-project", "12", "-number", "3"}, "update", ""},
		{false, []string{"reopen", "-project", "12", "-number", "3"}, "update", ""},
		{false, []string{"note", "-project", "12", "-number", "3", "-body-file", "-"}, "update", "From stdin"},
		{true, []string{"latest", "-project", "12"}, "handoff_read", ""},
		{true, []string{"show", "-project", "12", "-key", "handoff-key"}, "handoff_read", ""},
		{true, []string{"write", "-project", "12", "-title", "Saved", "-next", "Run checks", "-stdin"}, "handoff_write", "From stdin"},
	} {
		var out, errs bytes.Buffer
		var code int
		if tc.handoff {
			code = runHandoff(tc.args, strings.NewReader("From stdin"), &out, &errs)
		} else {
			code = runTaskInput(tc.args, strings.NewReader("From stdin"), &out, &errs)
		}
		if code != 0 || out.Len() == 0 {
			t.Fatalf("%v: %d %s", tc.args, code, errs.String())
		}
		got := requests[len(requests)-1]
		if got.Project != 12 || got.Operation != tc.operation {
			t.Fatalf("%v sent %+v", tc.args, got)
		}
		if tc.operation == "handoff_write" {
			if got.Handoff.Markdown != tc.body || got.Handoff.Next != "Run checks" {
				t.Fatalf("handoff body lost: %+v", got)
			}
		} else if tc.body != "" && (got.Edit.Markdown == nil || *got.Edit.Markdown != tc.body) {
			t.Fatalf("task body lost: %+v", got)
		}
	}
	if got := requests[2].Edit; got.Priority == nil || *got.Priority != "HIGH" || got.Tags == nil || len(*got.Tags) != 2 || !got.Replace {
		t.Fatalf("create fields: %+v", got)
	}
	if got := requests[3].Edit; got.Status == nil || *got.Status != "deferred" || got.BlockedBy == nil || len(*got.BlockedBy) != 2 || got.External == nil || got.Section != "Plan" {
		t.Fatalf("update fields: %+v", got)
	}
	if *requests[4].Edit.Status != "done" || *requests[5].Edit.Status != "open" {
		t.Fatal("close/reopen statuses lost")
	}
}

func TestRegistryCLIRefusesMissingBodiesAndFileWriter(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "this project uses cctm files", 409) }))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	for _, args := range [][]string{{"create", "-project", "1", "-title", "Work"}, {"note", "-project", "1", "-number", "1"}, {"update", "-project", "1", "-number", "1", "-replace"}, {"create", "-project", "1", "-title", "Work", "-body-file", "/missing/body"}, {"note", "-project", "1", "-number", "1", "-stdin", "-body-file", "-"}} {
		var out, errs bytes.Buffer
		if code := runTaskInput(args, strings.NewReader("Body"), &out, &errs); code == 0 || errs.Len() == 0 {
			t.Fatalf("%v: %d %s", args, code, errs.String())
		}
		if args[0] == "create" && len(args) == 5 && !strings.Contains(errs.String(), "cctm files") {
			t.Fatalf("file writer refusal lost: %s", errs.String())
		}
	}
	for _, args := range [][]string{nil, {"show", "-project", "1"}, {"write", "-project", "1", "-title", "Saved"}} {
		var out, errs bytes.Buffer
		if code := runHandoff(args, strings.NewReader("ignored"), &out, &errs); code == 0 || errs.Len() == 0 {
			t.Fatalf("handoff %v: %d %s", args, code, errs.String())
		}
	}
}
