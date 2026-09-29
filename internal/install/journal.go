package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Journal is the full account of a run: every command with its whole output,
// where the feed shows a tail. It lies in log/ of the installer's directory,
// a file a run, named by when the run began and what it was.
//
// A secret never reaches it. The installer keeps secrets out of argv and out
// of what it prints, and on top of that every value handed to Hide is cut
// out of whatever the journal is given, so a secret echoed back by a command
// is not written either.
type Journal struct {
	Path   string
	f      *os.File
	hidden []string
}

// OpenJournal starts the journal of a run of cmd in the installer's
// directory dir.
func OpenJournal(dir, cmd string, now time.Time) (*Journal, error) {
	logs := filepath.Join(dir, "log")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(logs, now.Format("20060102-150405")+"-"+cmd+".log")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return &Journal{Path: path, f: f}, nil
}

// Hide names a secret the journal must never hold.
func (j *Journal) Hide(secret string) {
	if secret != "" {
		j.hidden = append(j.hidden, secret)
	}
}

func (j *Journal) clean(s string) string {
	for _, h := range j.hidden {
		s = strings.ReplaceAll(s, h, "[hidden]")
	}
	return s
}

// Line writes a line of the run's own.
func (j *Journal) Line(format string, args ...any) error {
	_, err := j.f.WriteString(j.clean(fmt.Sprintf(format, args...)) + "\n")
	return err
}

// Command writes a command that ran, its output whole, and how it ended.
func (j *Journal) Command(argv []string, output string, err error) error {
	var b strings.Builder
	b.WriteString("$ " + strings.Join(argv, " ") + "\n")
	if output != "" {
		b.WriteString(output)
		if !strings.HasSuffix(output, "\n") {
			b.WriteString("\n")
		}
	}
	if err != nil {
		b.WriteString("! " + err.Error() + "\n")
	}
	_, werr := j.f.WriteString(j.clean(b.String()))
	return werr
}

// Close ends the journal.
func (j *Journal) Close() error { return j.f.Close() }
