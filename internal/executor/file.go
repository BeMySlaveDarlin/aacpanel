package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
)

const (
	filesEnv     = "AACP_FILES"
	fileTTL      = 7 * 24 * time.Hour
	fileMode     = 0o600
	filesDirMode = 0o700
)

func (e *Executor) sessionFile(ctx context.Context, target, caption string, files []action.File) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no file arrived: there is nothing to send")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}

	paths := make([]string, 0, len(files))
	for i := range files {
		path, err := storeFile(&files[i])
		if err != nil {
			return "", fmt.Errorf("%w%s", err, landed(paths))
		}
		paths = append(paths, path)
	}

	text := strings.Join(paths, "\n")
	if caption != "" {
		text = caption + "\n" + text
	}
	detail, err := deliverText(ctx, s, senderName, text)
	if err != nil {
		return "", fmt.Errorf("%w%s", err, landed(paths))
	}
	return detail + " · " + describeFiles(files, paths), nil
}

func describeFiles(files []action.File, paths []string) string {
	if len(files) == 1 {
		return fmt.Sprintf("file %s (%s) is at %s", files[0].Name, size(len(files[0].Data)), paths[0])
	}
	total := 0
	names := make([]string, 0, len(files))
	for _, f := range files {
		total += len(f.Data)
		names = append(names, fmt.Sprintf("%s (%s)", f.Name, size(len(f.Data))))
	}
	return fmt.Sprintf("%d files, %s: %s — in %s",
		len(files), size(total), strings.Join(names, ", "), filepath.Dir(paths[0]))
}

func landed(paths []string) string {
	switch len(paths) {
	case 0:
		return ""
	case 1:
		return "; the file itself was saved to " + paths[0]
	default:
		return fmt.Sprintf("; what was saved to disk: %s", strings.Join(paths, ", "))
	}
}

func storeFile(file *action.File) (string, error) {
	dir := filesDir()
	if err := os.MkdirAll(dir, filesDirMode); err != nil {
		return "", fmt.Errorf("the directory for files was not created: %w", err)
	}
	if err := os.Chmod(dir, filesDirMode); err != nil {
		return "", fmt.Errorf("permissions of the directory for files: %w", err)
	}
	sweepOldFiles(dir)

	var buf [3]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("the file name was not built: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s", time.Now().Format("20060102-150405"), hex.EncodeToString(buf[:]), file.Name)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, file.Data, fileMode); err != nil {
		return "", fmt.Errorf("the file was not written: %w", err)
	}
	return path, nil
}

func sweepOldFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	edge := time.Now().Add(-fileTTL)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(edge) {
			continue
		}
		os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func filesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return filesDirIn(os.Getenv, home)
}

func filesDirIn(get func(string) string, home string) string {
	if dir := get(filesEnv); dir != "" {
		return dir
	}
	if dir := get("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "aacpanel-exec", "files")
	}
	if home == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("aacpanel-exec-files-%d", os.Getuid()))
	}
	return filepath.Join(home, ".local", "share", "aacpanel-exec", "files")
}

func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
