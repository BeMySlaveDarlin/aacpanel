package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden file of the refusals")

// bareImage is the machine of the refusals: a system of the family with
// nothing the panel needs — no systemd, docker, claude, sudo, python3, tmux
// or jq.
const bareImage = "ubuntu:24.04"

// TestTheRefusalsOfABareMachine runs plan --plain on a bare Ubuntu in a
// container, as its first user, with the network cut off, and holds the
// stops it prints against the golden file. The passes are left out: free
// room and memory are the host's and change from run to run.
func TestTheRefusalsOfABareMachine(t *testing.T) {
	if err := exec.Command("docker", "version").Run(); err != nil {
		t.Skipf("docker does not answer here, and the bare machine is a container: %v", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "aacpanel-install")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the installer did not build: %v\n%s", err, out)
	}
	clone := filepath.Join(dir, "clone")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "docker-compose.yml"} {
		if err := os.WriteFile(filepath.Join(clone, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The clone is copied in and given to the user inside, as a clone of
	// that user would be; the installer runs as that user, not as root.
	run := exec.Command("docker", "run", "--rm", "--init", "--network", "none",
		"-v", bin+":/aacpanel-install:ro", "-v", clone+":/src:ro",
		"-e", "HOME=/home/ubuntu", "-e", "AACP_INSTALL_CLONE=/home/ubuntu/aacpanel",
		bareImage, "sh", "-c",
		"cp -r /src /home/ubuntu/aacpanel && chown -R ubuntu:ubuntu /home/ubuntu/aacpanel && "+
			"exec setpriv --reuid=ubuntu --regid=ubuntu --init-groups /aacpanel-install plan --plain")
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	err := run.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("plan on the bare machine ended with %v, want status 1:\n%s%s", err, stdout.String(), stderr.String())
	}
	got := strings.Join(stops(stdout.String()), "\n") + "\n"
	path := filepath.Join("testdata", "bare-ubuntu-24.04.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — run go test ./cmd/aacpanel-install -run Bare -update to write it", err)
	}
	if got != string(want) {
		t.Errorf("the stops of a bare machine differ from the golden file; plan said:\n%s", stdout.String())
	}
}

// stops takes the stop lines out of the plain view, each whole: a line the
// view wrapped goes on under its text, two columns in from its mark.
func stops(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		body := strings.TrimPrefix(strings.TrimPrefix(line, "  ⎿  "), "     ")
		switch {
		case strings.HasPrefix(body, "✓ "), strings.HasPrefix(body, "⚠ "), strings.HasPrefix(body, "✗ "):
			items = append(items, body)
		case strings.HasPrefix(line, "       ") && len(items) > 0:
			items[len(items)-1] += " " + strings.TrimSpace(line)
		}
	}
	var out2 []string
	for _, it := range items {
		if s, ok := strings.CutPrefix(it, "✗ "); ok {
			out2 = append(out2, s)
		}
	}
	return out2
}
