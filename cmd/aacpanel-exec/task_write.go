package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/session"
	"aacpanel/internal/taskreg"
)

func runTaskInput(args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "switch", "next", "create", "update", "close", "reopen", "note":
			return runRegistryCLI(false, args, in, out, stderr)
		}
	}
	return runTask(args, out, stderr)
}

func runHandoff(args []string, in io.Reader, out, stderr io.Writer) int {
	return runRegistryCLI(true, args, in, out, stderr)
}

func runRegistryCLI(handoff bool, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: aacpanel-exec handoff latest|show|write -project ID [flags]")
		return 2
	}
	command := args[0]
	known := []string{"switch", "next", "create", "update", "close", "reopen", "note"}
	if handoff {
		known = []string{"latest", "show", "write"}
	}
	valid := false
	for _, name := range known {
		valid = valid || name == command
	}
	if !valid {
		fmt.Fprintln(stderr, "unknown registry command: "+command)
		return 2
	}
	flags := flag.NewFlagSet("task registry", flag.ContinueOnError)
	flags.SetOutput(stderr)
	project := flags.Int("project", 0, "project id from the map")
	conversation := flags.String("conversation", "", "agent conversation; omitted records the person")
	number := flags.Int64("number", 0, "task number in the project")
	title := flags.String("title", "", "task or handoff title")
	status := flags.String("status", "", "task status")
	priority := flags.String("priority", "", "HIGH, MEDIUM, LOW or empty")
	order := flags.Float64("order", 0, "position within the priority group")
	tags := flags.String("tags", "", "comma-separated tags; empty clears them")
	blocked := flags.String("blocked-by", "", "comma-separated blocking task numbers; empty clears them")
	external := flags.String("external", "", "external issue URL or key; empty clears it")
	file := flags.String("body-file", "", "read markdown from this file; - reads stdin explicitly")
	stdin := flags.Bool("stdin", false, "read markdown from stdin explicitly")
	replace := flags.Bool("replace", false, "replace the whole task body")
	section := flags.String("section", "", "heading of an appended section; Notes by default")
	next := flags.String("next", "", "handoff's next step")
	key := flags.String("key", "", "handoff key")
	query := flags.String("q", "", "text in task title or body")
	tag := flags.String("tag", "", "filter tasks by tag")
	limit := flags.Int("limit", 100, "page size, at most 500")
	rest := args[1:]
	// A switch names its project directly as well as through -project.
	var switchProject int
	if command == "switch" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		n, err := strconv.Atoi(rest[0])
		if err != nil || n <= 0 {
			fmt.Fprintln(stderr, "switch needs a positive project id")
			return 2
		}
		switchProject = n
		rest = rest[1:]
	}
	if err := flags.Parse(rest); err != nil {
		return 2
	}
	if switchProject > 0 {
		if *project != 0 && *project != switchProject {
			fmt.Fprintln(stderr, "conflicting project ids")
			return 2
		}
		*project = switchProject
	}
	if flags.NArg() != 0 || *project < 0 || (*project == 0 && *conversation == "") || *number < 0 || *limit <= 0 {
		fmt.Fprintln(stderr, "the registry needs a project or conversation and valid arguments")
		return 2
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	allowed := " project conversation "
	switch {
	case handoff && command == "write":
		allowed += "title next body-file stdin "
	case handoff && command == "show":
		allowed += "key "
	case handoff:
	case command == "switch":
	case command == "create":
		allowed += "title status priority order tags blocked-by external body-file stdin replace section "
	case command == "next":
		allowed += "tag q status limit "
	default:
		allowed += "number title status priority order tags blocked-by external body-file stdin replace section "
	}
	for name := range set {
		if !strings.Contains(allowed, " "+name+" ") {
			fmt.Fprintln(stderr, "flag -"+name+" does not apply to "+command)
			return 2
		}
	}
	edit := taskreg.Edit{Replace: *replace, Section: *section}
	if set["title"] {
		edit.Title = title
	}
	if set["status"] {
		edit.Status = status
	}
	if set["priority"] {
		edit.Priority = priority
	}
	if set["order"] {
		edit.Order = order
	}
	if set["external"] {
		edit.External = external
	}
	if set["tags"] {
		v := splitRegistryList(*tags)
		edit.Tags = &v
	}
	if set["blocked-by"] {
		v := []int64{}
		for _, text := range splitRegistryList(*blocked) {
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil || n <= 0 {
				fmt.Fprintln(stderr, "blocked-by needs positive numbers")
				return 2
			}
			v = append(v, n)
		}
		edit.BlockedBy = &v
	}
	if set["body-file"] && *file == "" {
		fmt.Fprintln(stderr, "body-file needs a path or - for stdin")
		return 2
	}
	if *file != "" && *stdin {
		fmt.Fprintln(stderr, "choose body-file or stdin, not both")
		return 2
	}
	if *file != "" || *stdin {
		body, err := registryBody(*file, in)
		if err != nil {
			fmt.Fprintln(stderr, "read markdown: "+err.Error())
			return 1
		}
		edit.Markdown = &body
	}
	request := taskreg.Request{Project: *project, Conversation: *conversation, Number: *number, Edit: edit, Key: *key, Limit: *limit, Text: *query}
	if *tag != "" {
		request.Tags = []string{*tag}
	}
	if *status != "" {
		request.Status = strings.Split(*status, ",")
	}
	switch {
	case handoff:
		switch command {
		case "latest", "show":
			request.Operation = "handoff_read"
			if command == "show" && *key == "" {
				fmt.Fprintln(stderr, "handoff show needs -key")
				return 2
			}
		case "write":
			request.Operation = "handoff_write"
			if edit.Markdown == nil {
				fmt.Fprintln(stderr, "handoff write needs -body-file or -stdin")
				return 2
			}
			request.Handoff = taskreg.HandoffInput{Title: *title, Markdown: *edit.Markdown, Next: *next}
		}
	case command == "next":
		request.Operation = "list"
		request.Next = true
	case command == "switch":
		request.Operation = "switch"
	case command == "create":
		request.Operation = "create"
	default:
		request.Operation = "update"
		if *number <= 0 {
			fmt.Fprintln(stderr, "task update, close, reopen and note need -number")
			return 2
		}
		switch command {
		case "close":
			v := "done"
			if *status != "" {
				v = *status
			}
			if v != "done" && v != "cancelled" {
				fmt.Fprintln(stderr, "close status must be done or cancelled")
				return 2
			}
			request.Edit.Status = &v
		case "reopen":
			v := "open"
			request.Edit.Status = &v
		case "note":
			if edit.Markdown == nil {
				fmt.Fprintln(stderr, "task note needs -body-file or -stdin")
				return 2
			}
		}
	}
	if request.Operation == "create" || request.Operation == "update" {
		if err := request.Edit.Validate(request.Operation == "create"); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var reply json.RawMessage
	if err := taskreg.Call(ctx, session.Here().Panel, request, &reply); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(reply); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func splitRegistryList(text string) []string {
	out := []string{}
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func registryBody(file string, in io.Reader) (string, error) {
	if file != "" && file != "-" {
		body, err := os.ReadFile(file)
		return string(body), err
	}
	body, err := io.ReadAll(in)
	return string(body), err
}
