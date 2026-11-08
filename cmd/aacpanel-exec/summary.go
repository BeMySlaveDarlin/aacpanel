package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"aacpanel/internal/launcher"
)

func runSummary(in io.Reader, out, errs io.Writer) int {
	if os.Geteuid() == 0 {
		fmt.Fprintln(errs, "aacpanel-exec: summarizing as root is not allowed")
		return 2
	}
	var spec launcher.SummarySpec
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&spec); err != nil {
		fmt.Fprintf(errs, "aacpanel-exec: the summary request was not parsed: %v\n", err)
		return 2
	}
	report, err := launcher.Summarize(context.Background(), spec)
	if err != nil {
		fmt.Fprintf(errs, "aacpanel-exec: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintf(errs, "aacpanel-exec: the summary was not sent: %v\n", err)
		return 1
	}
	return 0
}
