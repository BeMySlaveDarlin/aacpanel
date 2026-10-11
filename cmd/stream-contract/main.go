// Command stream-contract checks the installed claude's stream protocol.
//
//	go run ./cmd/stream-contract [-claude PATH] [-model haiku] [-json] [-keep]
//
// It spends tokens and stays outside make check. Exit codes: 0 when every
// required check holds, 1 on a broken contract, 2 when the session cannot start.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"aacpanel/internal/stream/contract"
)

func main() {
	claude := flag.String("claude", "claude", "the claude program")
	model := flag.String("model", "haiku", "the model for short live turns")
	asJSON := flag.Bool("json", false, "the verdicts as JSON")
	keep := flag.Bool("keep", false, "keep the directory, event log and transcript")
	flag.Parse()
	report, code := contract.Run(contract.Options{Claude: *claude, Model: *model, Keep: *keep}, func(line string) {
		if !*asJSON {
			fmt.Fprintln(os.Stderr, line)
		}
	})
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", " ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 2
		}
	} else {
		fmt.Print(report.Text())
	}
	os.Exit(code)
}
