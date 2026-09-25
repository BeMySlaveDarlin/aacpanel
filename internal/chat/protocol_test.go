package chat

import (
	"encoding/json"
	"testing"
)

func TestReplyKeepsEveryFieldAgentSends(t *testing.T) {
	const fromAgent = `{
		"ok": true,
		"session": "567f4d24-cd5f-48fa-bdc1-04c89d203494",
		"items": [
			{"role": "me", "text": "hi", "at": "2026-08-24T10:00:00Z", "pos": 10, "state": "queued", "cut": true},
			{"role": "ai", "text": "the answer", "at": "2026-08-24T10:00:01Z", "pos": 20,
			 "files": [{"path": "web/src/api.js", "name": "api.js", "size": 8123},
			           {"path": "docs/scheme.png", "name": "scheme.png", "size": 42000, "media": "image/png"}]},
			{"role": "note", "text": "the human interrupted the answer", "at": "2026-08-24T10:00:02Z", "pos": 30},
			{"role": "shots", "at": "2026-08-24T10:00:03Z", "pos": 40,
			 "shots": [{"index": 0, "media": "image/png", "bytes": 512}]},
			{"role": "tools", "at": "2026-08-24T10:00:04Z", "pos": 50, "kind": "bash", "run": 50,
			 "calls": [{"name": "Bash", "arg": "ls -la", "at": "2026-08-24T10:00:04Z", "seq": 0, "pos": 50, "index": 0}]},
			{"role": "tools", "at": "2026-08-24T10:00:05Z", "pos": 60, "kind": "files", "run": 50,
			 "calls": [{"name": "Read", "arg": "/etc/hosts", "at": "2026-08-24T10:00:05Z", "seq": 1, "pos": 60, "index": 0}]},
			{"role": "mail", "from": "phase-hint", "source": "agent", "text": "done", "at": "2026-08-24T10:00:06Z", "pos": 70},
			{"role": "mail", "from": "notes", "source": "session", "text": "got it", "at": "2026-08-24T10:00:06Z", "pos": 71},
			{"role": "mail", "from": "review-draft", "source": "agent", "dir": "out", "text": "send the verdict", "at": "2026-08-24T10:00:06Z", "pos": 72},
			{"role": "mind", "text": "The socket does not answer — I will check the unit", "cut": true, "at": "2026-08-24T10:00:07Z", "pos": 75},
			{"role": "think", "count": 3, "tokens": 812, "run": 50, "at": "2026-08-24T10:00:07Z", "pos": 80,
			 "spots": [{"seq": 2, "at": "2026-08-24T10:00:07Z", "tokens": 812, "pos": 80, "index": 0}]},
			{"role": "tools", "at": "2026-08-24T10:00:08Z", "pos": 90, "kind": "bash", "run": 90,
			 "calls": [{"name": "Skill: finalize", "arg": "make", "at": "2026-08-24T10:00:08Z", "seq": 0, "pos": 90, "index": 0, "use": "toolu_01VbcX"}]},
			{"role": "artifact", "use": "toolu_01Art", "title": "The panel — UI sketches", "file": "panel-ui.html",
			 "desc": "Client mockups", "icon": "🧭", "label": "tiles", "note": "tiles by project",
			 "at": "2026-08-24T10:00:09Z", "pos": 95},
			{"role": "artifactlink", "use": "toolu_01Art", "url": "https://claude.ai/code/artifact/0a1b2c3d",
			 "at": "2026-08-24T10:00:09Z", "pos": 96},
			{"role": "brief", "use": "toolu_01Bash", "id": "seven-after-twelve",
			 "title": "Seven questions after twelve", "eyebrow": "after the review", "questions": 7,
			 "at": "2026-08-24T10:00:09Z", "pos": 97},
			{"role": "taskdone", "use": "toolu_01VbcX", "status": "completed", "summary": "Background command \"make\" completed", "at": "2026-08-24T10:00:09Z", "pos": 100},
			{"role": "asked", "use": "toolu_01Ask", "at": "2026-08-24T10:00:10Z", "pos": 110,
			 "asked": [{"text": "How do we bring the stack down?", "header": "Way", "answer": ["one by one"]},
			           {"text": "Wait for the children?", "header": "Children"}]},
			{"role": "asked", "use": "toolu_01Ask2", "status": "afk", "at": "2026-08-24T10:00:11Z", "pos": 120,
			 "asked": [{"text": "Do we deploy?", "header": "Deploy"}]},
			{"role": "wake", "text": "Keep the loop going", "at": "2026-08-24T10:00:12Z", "pos": 130, "fixes": "me"},
			{"role": "shell", "text": "make check", "at": "2026-08-24T10:00:13Z", "pos": 140},
			{"role": "shellout", "text": "ok", "err": "warning: the base is not up", "cut": true, "at": "2026-08-24T10:00:14Z", "pos": 141},
			{"role": "command", "name": "context", "at": "2026-08-24T10:00:15Z", "pos": 150,
			 "data": {"model": "claude-opus-5-5[1m]", "used": 263000, "max": 1000000, "percent": 26,
			          "categories": [{"name": "Messages", "kind": "used", "tokens": 224600},
			                         {"name": "MCP tools (deferred)", "kind": "deferred", "tokens": 0, "under": 20}]}},
			{"role": "permitted", "at": "2026-08-24T10:00:16Z", "pos": 160,
			 "rows": [{"tool": "Bash", "subject": "git push origin main", "decision": "allow", "lasting": true},
			          {"tool": "WebFetch", "decision": "deny"}]},
			{"role": "taskdone", "use": "toolu_01Agent", "status": "completed", "summary": "Agent \"notes\" finished", "task": "a515a204cce27a85c",
			 "ms": 24233, "tokens": 33108, "at": "2026-08-24T10:00:17Z", "pos": 170},
			{"role": "notice", "text": "Unknown command: /storage", "from": "safeguards", "level": "warn", "cut": true,
			 "at": "2026-08-24T10:00:18Z", "pos": 180},
			{"role": "turn", "ms": 169000, "agents": 2, "at": "2026-08-24T10:00:19Z", "pos": 190}
		],
		"total": 14, "moreBefore": true, "first": 10, "last": 100, "size": 4096,
		"text": "the tail of the command output", "cut": true,
		"letters": [{"at": "2026-08-24T10:00:10Z", "text": "done, no findings"}]
	}`

	var reply Reply
	if err := json.Unmarshal([]byte(fromAgent), &reply); err != nil {
		t.Fatalf("the agent answer does not parse: %v", err)
	}

	var was, now any
	if err := json.Unmarshal([]byte(fromAgent), &was); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("the answer is not serialized: %v", err)
	}
	if err := json.Unmarshal(out, &now); err != nil {
		t.Fatal(err)
	}
	missing(t, "", was, now)
}

func TestCallReplyKeepsEveryField(t *testing.T) {
	for name, fromAgent := range map[string]string{
		"a call": `{
			"ok": true,
			"tool": "mcp__docker__docker_exec",
			"args": "{\n  \"command\": \"ls\"\n}", "argsCut": true,
			"at": "2026-09-07T10:00:00Z",
			"result": "total 0", "resultCut": true, "failed": true,
			"resultAt": "2026-09-07T10:00:01Z"
		}`,
		"a call with no result": `{
			"ok": true, "tool": "Bash", "args": "{}",
			"at": "2026-09-07T10:00:00Z", "pending": true
		}`,
	} {
		t.Run(name, func(t *testing.T) {
			var reply Reply
			if err := json.Unmarshal([]byte(fromAgent), &reply); err != nil {
				t.Fatalf("the agent answer does not parse: %v", err)
			}
			var was, now any
			if err := json.Unmarshal([]byte(fromAgent), &was); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(reply.Call)
			if err != nil {
				t.Fatalf("the answer is not serialized: %v", err)
			}
			if err := json.Unmarshal(out, &now); err != nil {
				t.Fatal(err)
			}
			delete(was.(map[string]any), "ok")
			missing(t, "", was, now)
		})
	}
}

func TestFileReplyKeepsEveryField(t *testing.T) {
	for name, fromAgent := range map[string]string{
		"a chunk of text": `{
			"ok": true, "kind": "text", "name": "app.css",
			"text": ".body { color: red }", "cut": true,
			"size": 131072, "offset": 0, "next": 65494
		}`,
		"an image": `{
			"ok": true, "kind": "image", "name": "scheme.png",
			"media": "image/png", "data": "iVBORw0KGgo=", "size": 42000
		}`,
		"the image is too big": `{
			"ok": true, "kind": "image", "name": "dump.png",
			"media": "image/png", "size": 9000000, "tooBig": true
		}`,
		"a binary file": `{
			"ok": true, "kind": "binary", "binary": true,
			"name": "bin.dat", "size": 3
		}`,
	} {
		t.Run(name, func(t *testing.T) {
			var reply Reply
			if err := json.Unmarshal([]byte(fromAgent), &reply); err != nil {
				t.Fatalf("the agent answer does not parse: %v", err)
			}
			var was, now any
			if err := json.Unmarshal([]byte(fromAgent), &was); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(reply)
			if err != nil {
				t.Fatalf("the answer is not serialized: %v", err)
			}
			if err := json.Unmarshal(out, &now); err != nil {
				t.Fatal(err)
			}
			missing(t, "", was, now)
		})
	}
}

func TestArchiveReplyKeepsEveryField(t *testing.T) {
	const fromAgent = `{
		"ok": true,
		"archive": {
			"total": 481, "limit": 20, "offset": 0,
			"rows": [{
				"sessionId": "0ecdd482-78bf-4d2c-beb8-fcb4f2feb9f7",
				"name": "u", "nameGuessed": true, "cwd": "/home/u", "home": true,
				"slug": "-home-u", "profile": "personal",
				"model": "claude-opus-5", "effort": "xhigh",
				"pct": 38.5, "pctMax": 76.7, "tokens": 385000, "tokensMax": 767000,
				"limit": 1000000, "limitKnown": true,
				"messages": 744, "compacts": 2, "stale": true,
				"startedAt": "2026-08-23T12:37:00Z", "lastAt": "2026-08-23T22:31:00Z",
				"lastRequestAt": "2026-08-23T22:30:00Z", "noRequests": true
			}]
		}
	}`

	var reply Reply
	if err := json.Unmarshal([]byte(fromAgent), &reply); err != nil {
		t.Fatalf("the agent answer does not parse: %v", err)
	}
	if reply.Archive == nil {
		t.Fatal("the archive page does not parse at all")
	}
	if reply.Archive.Total != 481 {
		t.Errorf("the page total is %d and not 481: the field collapsed into the feed total", reply.Archive.Total)
	}

	var was, now any
	if err := json.Unmarshal([]byte(fromAgent), &was); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("the answer is not serialized: %v", err)
	}
	if err := json.Unmarshal(out, &now); err != nil {
		t.Fatal(err)
	}
	missing(t, "", was, now)
}

func TestStateReplyKeepsEveryField(t *testing.T) {
	const fromAgent = `{
		"ok": true,
		"session": "567f4d24-cd5f-48fa-bdc1-04c89d203494",
		"items": [], "total": 0, "moreBefore": false, "first": null, "last": null,
		"state": {
			"tasks": [{"id": "b0g4knhe1", "text": "Waiting for CI", "at": "2026-08-25T10:00:00Z", "kind": "bash", "event": "2026-08-25T10:19:00Z", "line": "gh run watch 4211"},
			          {"id": "wakeup", "text": "watching CI", "at": "2026-08-25T10:01:00Z", "kind": "wake", "due": "2026-08-25T10:21:00Z"}],
			"agents": [{"name": "audit-rules", "text": "rules audit", "at": "2026-08-25T10:00:00Z",
			              "status": "active", "model": "opus[1m]", "color": "cyan",
			              "last": "2026-08-25T10:04:00Z",
			              "id": "aaudit-rules-0123456789abcdef", "kind": "subagent",
			              "tokens": 175424, "limit": 1000000, "limitKnown": true},
			             {"name": "audit-docs", "text": "docs audit", "at": "2026-08-25T10:01:00Z",
			              "status": "reported", "reportedAt": "2026-08-25T10:03:00Z",
			              "model": "sonnet", "color": "pink", "last": "2026-08-25T10:03:00Z",
			              "id": "aaudit-docs-fedcba9876543210", "kind": "teammate"},
			             {"name": "cx-scout", "text": "Scouting the restore", "at": "2026-08-25T10:02:00Z",
			              "status": "completed", "doneAt": "2026-08-25T10:12:00Z",
			              "model": "claude-sonnet-5", "last": "2026-08-25T10:12:00Z",
			              "id": "adf5ce617c6afff7a", "kind": "background",
			              "tokens": 51030, "limit": 200000, "limitKnown": true}],
			"workflows": [{"id": "wf_0c96efa8d5b", "task": "wthbimhp4", "name": "review-changes",
			               "text": "Review the diff across dimensions", "at": "2026-08-25T10:00:00Z",
			               "status": "completed", "doneAt": "2026-08-25T10:41:00Z",
			               "event": "2026-08-25T10:20:00Z",
			               "dir": "/home/u/.claude/projects/-opt-p/567f/subagents/workflows/wf_0c96efa8d5b",
			               "script": "/home/u/.claude/projects/-opt-p/567f/workflows/scripts/review-changes.js",
			               "agents": 9, "tokens": 250000, "calls": 61, "ms": 743000,
			               "phases": [{"title": "Review", "detail": "five dimensions"},
			                          {"title": "Verify"}],
			               "logs": ["[stall] agent \"review:bugs\" stalled after 339s — retrying (1/5)"],
			               "result": "{\n \"confirmed\": 3\n}"}],
			"artifacts": [{"path": "/opt/p/plan.html", "file": "plan.html", "title": "Wave plan",
			               "desc": "Epics and dates", "label": "wave-3", "note": "fixes after review",
			               "icon": "🧭", "url": "https://claude.ai/code/artifact/0a1b2c3d",
			               "at": "2026-08-25T10:02:00Z", "count": 3}],
			"docs": [{"path": "/opt/p/docs/tz.md", "file": "tz.md", "dir": "docs",
			          "at": "2026-08-25T10:05:00Z", "count": 7}],
			"sent": [{"path": "/opt/p/cv/resume.pdf", "file": "resume.pdf", "size": 89537,
			          "media": "application/pdf", "at": "2026-08-25T10:06:00Z", "count": 2}],
			"ask": {
				"sessionId": "567f4d24-cd5f-48fa-bdc1-04c89d203494",
				"toolUseId": "toolu_01AaBbCcDdEeFfGgHhJjKkLm",
				"cwd": "/opt/x", "at": "2026-08-25T10:05:00Z",
				"questions": [{
					"text": "Do we fix now or after the deploy?",
					"header": "Order", "multi": false,
					"options": [
						{"label": "Now", "description": "The deploy waits"},
						{"label": "Later", "description": "We deploy first", "preview": "+---+\n| A |\n+---+"}
					]
				}]
			}
		}
	}`

	var reply Reply
	if err := json.Unmarshal([]byte(fromAgent), &reply); err != nil {
		t.Fatalf("the agent answer does not parse: %v", err)
	}
	if reply.State == nil {
		t.Fatal("the session state does not parse at all")
	}
	if len(reply.State.Tasks) != 2 || len(reply.State.Agents) != 3 {
		t.Fatalf("the state arrived incomplete: %+v", *reply.State)
	}
	if tk := reply.State.Tasks[1]; tk.Kind != "wake" || tk.Due == "" {
		t.Errorf("the task arrived without a kind or a due time: %+v", tk)
	}
	if tk := reply.State.Tasks[0]; tk.Event == "" {
		t.Errorf("the task arrived without the time of its last event: %+v", tk)
	}
	if a := reply.State.Agents[0]; a.Tokens != 175424 || a.Limit != 1000000 || !a.LimitKnown {
		t.Errorf("the context of the agent did not survive the decoding: %+v", a)
	}
	if a := reply.State.Agents[0]; a.Model == "" || a.Color == "" || a.Last == "" {
		t.Errorf("the agent arrived without its harness data: %+v", a)
	}
	if a := reply.State.Agents[1]; a.Status != "reported" || a.ReportedAt == "" {
		t.Errorf("the agent that has reported lost its status: %+v", a)
	}
	if len(reply.State.Artifacts) != 1 || len(reply.State.Docs) != 1 {
		t.Fatalf("the artifacts or the docs are lost: %+v", *reply.State)
	}
	if a := reply.State.Artifacts[0]; a.Title == "" || a.URL == "" || a.Count != 3 || a.Icon == "" {
		t.Errorf("the artifact arrived incomplete: %+v", a)
	}
	if d := reply.State.Docs[0]; d.Path == "" || d.Dir == "" || d.Count != 7 {
		t.Errorf("the doc arrived incomplete: %+v", d)
	}
	if len(reply.State.Sent) != 1 {
		t.Fatalf("the sent files are lost: %+v", *reply.State)
	}
	if f := reply.State.Sent[0]; f.Path == "" || f.File == "" || f.Size != 89537 || f.Media == "" || f.Count != 2 {
		t.Errorf("the sent file arrived incomplete: %+v", f)
	}
	if reply.State.Ask == nil || len(reply.State.Ask.Questions) != 1 {
		t.Fatalf("the session question did not make it through: %+v", reply.State.Ask)
	}
	if len(reply.State.Ask.Questions[0].Options) != 2 {
		t.Errorf("the answer options are lost: %+v", reply.State.Ask.Questions[0])
	}
	if got := reply.State.Ask.Questions[0].Options[1].Preview; got != "+---+\n| A |\n+---+" {
		t.Errorf("the option preview is lost or reshaped: %q", got)
	}

	var was, now any
	if err := json.Unmarshal([]byte(fromAgent), &was); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("the answer is not serialized: %v", err)
	}
	if err := json.Unmarshal(out, &now); err != nil {
		t.Fatal(err)
	}
	missing(t, "", was, now)
}

func missing(t *testing.T, path string, was, now any) {
	t.Helper()
	switch old := was.(type) {
	case map[string]any:
		fresh, ok := now.(map[string]any)
		if !ok {
			t.Errorf("%s: was an object, now %T", path, now)
			return
		}
		for key, value := range old {
			got, ok := fresh[key]
			if !ok {
				t.Errorf("%s.%s: the field is lost on the way through the structs of the service", path, key)
				continue
			}
			missing(t, path+"."+key, value, got)
		}
	case []any:
		fresh, ok := now.([]any)
		if !ok || len(fresh) != len(old) {
			t.Errorf("%s: the list changed: was %d, now %v", path, len(old), now)
			return
		}
		for i := range old {
			missing(t, path+"[]", old[i], fresh[i])
		}
	default:
		if was != now {
			t.Errorf("%s: the value changed: was %v, now %v", path, was, now)
		}
	}
}
