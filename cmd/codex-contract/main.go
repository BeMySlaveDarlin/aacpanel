// Command codex-contract checks the app-server protocol of the installed codex
// against what the panel reads and sends of it.
//
//	go run ./cmd/codex-contract [-codex PATH] [-schema DIR] [-json] [-write]
//
// The program is the one the executor starts: AACP_CODEX, or codex from PATH.
// It writes the schema of its protocol, the experimental part with it — the
// panel asks for that part at the handshake — and asks codex in CODEX_HOME
// (or ~/.codex) whether it trusts and enables the installed panel hooks. With
// no daemon socket, the same program runs as a private stdio app-server for
// the check. No thread is loaded and no tokens are spent. With -schema,
// only the saved schema is checked; the live hook check is not run.
//
// Exit code 0 when nothing the panel reads or sends is gone or changed, 1 when
// something is, 2 when the schema could not be had at all.
//
// -write puts the schema, cut to what the panel uses, in place of the
// reference, once the report has been read. A release that took something the
// panel uses is not written: the panel is brought to it first. Nor is a
// -schema directory: it does not say which release wrote it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/codex/contract"
	"aacpanel/internal/install"
	"aacpanel/internal/webbuild"
)

// codexEnv names the codex program the way the machine description names it
// for the executor.
const codexEnv = "AACP_CODEX"

// How long codex may take to say its version and to write its schema.
const runWait = time.Minute

func main() {
	program := os.Getenv(codexEnv)
	if program == "" {
		program = "codex"
	}
	codex := flag.String("codex", program, "the codex program; "+codexEnv+" or codex from PATH")
	schema := flag.String("schema", "", "a directory codex has already written its schema into, in place of running codex")
	asJSON := flag.Bool("json", false, "the report as JSON")
	write := flag.Bool("write", false, "write the schema, cut to what the panel uses, as the reference")
	flag.Parse()
	os.Exit(run(*codex, *schema, *asJSON, *write))
}

func run(codex, schemaDir string, asJSON, write bool) int {
	live := schemaDir == ""
	panel, err := contract.Panel()
	if err != nil {
		return say(2, "the contract of the panel does not read: %v", err)
	}
	// The reference names the release it is of, and a directory of schema
	// does not say which codex wrote it: the codex installed now may be
	// another, and the name of the directory is a path of this machine, which
	// the tree does not keep.
	if write && schemaDir != "" {
		return say(2, "-write takes a release of codex as the reference, and a directory of schema does not say "+
			"which release wrote it: run without -schema, and codex writes its schema and names its version")
	}
	ref, err := contract.Reference()
	if err != nil {
		return say(2, "the reference does not read: %v", err)
	}
	version := "the schema in " + schemaDir
	if schemaDir == "" {
		dir, err := os.MkdirTemp("", "codex-contract-")
		if err != nil {
			return say(2, "no directory for the schema: %v", err)
		}
		defer os.RemoveAll(dir)
		if version, err = generate(codex, dir); err != nil {
			return say(2, "%v", err)
		}
		schemaDir = dir
	}
	data, err := os.ReadFile(filepath.Join(schemaDir, contract.BundleFile))
	if err != nil {
		return say(2, "codex wrote no bundle of its schema: %v", err)
	}
	cur, err := contract.Load(data)
	if err != nil {
		return say(2, "%v", err)
	}

	report := contract.Check(cur, ref, panel)
	report.Codex = version
	if live {
		f := contract.Finding{Place: "hooks/list", Detail: "the installed panel hooks are present, trusted and enabled"}
		if source, err := checkHooks(codex); err != nil {
			f.Detail, f.Red = err.Error(), true
		} else {
			f.Detail = source + "; " + f.Detail
		}
		report.Hooks = []contract.Finding{f}
	} else {
		report.Hooks = []contract.Finding{{Place: "hooks/list", Detail: "not checked: -schema checks only the saved protocol"}}
	}
	if asJSON {
		out, _ := json.MarshalIndent(report, "", " ")
		fmt.Println(string(out))
	} else {
		fmt.Print(report.Text())
	}
	if !write {
		if report.Red() {
			return 1
		}
		return 0
	}
	if report.Red() {
		return say(1, "the reference is left as it is: the protocol or hook trust check failed")
	}
	return writeReference(cur, panel, version)
}

func checkHooks(program string) (string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".codex")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	hooks, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil {
		return "", fmt.Errorf("the installed codex hooks cannot be read: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runWait)
	defer cancel()
	return install.CheckCodexHooks(ctx, program, home, hooks)
}

// generate has codex write the schema of its protocol and returns its version.
func generate(codex, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, codex, "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("no codex to ask: %s is not found — name the program with -codex or %s", codex, codexEnv)
		}
		return "", fmt.Errorf("%s --version: %v", codex, err)
	}
	version := strings.TrimSpace(string(out))
	cmd := exec.CommandContext(ctx, codex, "app-server", "generate-json-schema", "--experimental", "--out", dir)
	if said, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s did not write its schema: %v\n%s", version, err, said)
	}
	return version, nil
}

func writeReference(cur *contract.Schema, panel *contract.Contract, version string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return say(2, "%v", err)
	}
	root, err := webbuild.FindRoot(cwd)
	if err != nil {
		return say(2, "%v", err)
	}
	body, err := contract.Prune(cur, panel, version)
	if err != nil {
		return say(2, "the reference was not made: %v", err)
	}
	path := filepath.Join(root, contract.ReferencePath)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return say(2, "%v", err)
	}
	fmt.Fprintf(os.Stderr, "the reference is now %s: %s\n", version, path)
	return 0
}

func say(code int, format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "codex-contract: "+format+"\n", args...)
	return code
}
