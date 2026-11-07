package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/session"
)

func runTask(args []string, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: aacpanel-exec task import|list|show [flags]")
		return 2
	}
	command := args[0]
	if command != "import" && command != "list" && command != "show" {
		fmt.Fprintln(stderr, "unknown task command: "+command)
		return 2
	}
	flags := flag.NewFlagSet("task "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	project := flags.Int("project", 0, "project id from the map")
	contour := flags.Int("contour", 0, "contour id from the map")
	number := flags.Int64("number", 0, "task number within the project")
	status := flags.String("status", "", "comma-separated task statuses")
	tag := flags.String("tag", "", "task tag")
	query := flags.String("q", "", "text in the title or body")
	external := flags.String("external", "", "external issue number")
	deleted := flags.Bool("deleted", false, "include tasks removed from the source")
	limit := flags.Int("limit", 100, "page size, at most 500")
	offset := flags.Int("offset", 0, "number of tasks to skip")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *project < 0 || *contour < 0 || *offset < 0 || *limit <= 0 {
		fmt.Fprintln(stderr, "invalid task arguments")
		return 2
	}
	values := url.Values{}
	if *project > 0 {
		values.Set("project", strconv.Itoa(*project))
	}
	method, path := http.MethodGet, "/api/tasks"
	switch command {
	case "import":
		method, path = http.MethodPost, "/api/tasks/import"
	case "show":
		if *project == 0 || *number <= 0 {
			fmt.Fprintln(stderr, "task show needs -project and -number")
			return 2
		}
		path = fmt.Sprintf("/api/projects/%d/tasks/%d", *project, *number)
	case "list":
		if *contour > 0 {
			values.Set("contour", strconv.Itoa(*contour))
		}
		for key, value := range map[string]string{"status": *status, "tag": *tag, "q": *query, "external": *external} {
			if value != "" {
				values.Set(key, value)
			}
		}
		if *deleted {
			values.Set("deleted", "true")
		}
		values.Set("limit", strconv.Itoa(*limit))
		values.Set("offset", strconv.Itoa(*offset))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(session.Here().Panel, "/")+path+"?"+values.Encode(), nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Fprintln(stderr, "the panel did not answer: "+err.Error())
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, "task request: "+response.Status)
		_, _ = io.Copy(stderr, io.LimitReader(response.Body, 64<<10))
		return 1
	}
	var body json.RawMessage
	if err = json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&body); err != nil {
		fmt.Fprintln(stderr, "the task reply did not parse: "+err.Error())
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(body); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
