package observer

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// readMax bounds what one look reads of a log: a command that printed
// megabytes at once is read on over the next looks rather than held whole.
const readMax = 256 << 10

// cursor is where the reading of a log stands: the file by its inode and the
// first byte of it not read yet.
type cursor struct {
	Inode  uint64
	Offset int64
}

func inodeOf(path string) (uint64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("the file system names no inode")
	}
	return st.Ino, info.Size(), nil
}

// readOn reads the whole lines of the log of dir past at and says where the
// reading stands after them. A line not finished yet is left for the next
// look, unless the command has ended — then nothing more comes to finish it.
// A file moved aside since the last look is read to its end first. A log
// moved aside twice since — an executor that was down while its command
// printed two files — goes on from the start of the file there now, and
// skipped says lines were never read. more says the log holds more than one
// look reads.
func readOn(dir string, at cursor, ended bool) (lines []string, next cursor, skipped, more bool, err error) {
	path := filepath.Join(dir, logFile)
	ino, _, err := inodeOf(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, at, false, false, nil
	}
	if err != nil {
		return nil, at, false, false, err
	}
	aside := false
	switch {
	case at.Inode == 0:
		// The first look at a command that printed a file already reads the
		// file moved aside first.
		at.Inode = ino
		if old, _, err := inodeOf(filepath.Join(dir, oldLogFile)); err == nil {
			path, aside, at.Inode = filepath.Join(dir, oldLogFile), true, old
		}
	case at.Inode == ino:
	default:
		old, _, err := inodeOf(filepath.Join(dir, oldLogFile))
		if err == nil && old == at.Inode {
			path, aside = filepath.Join(dir, oldLogFile), true
		} else {
			at, skipped = cursor{Inode: ino}, true
		}
	}
	data, eof, err := readAt(path, at.Offset)
	if err != nil {
		return nil, at, skipped, false, err
	}
	next = at
	if aside && eof {
		// The file moved aside takes nothing more: what is left of a line in
		// it is all of that line the log has, and the file there now is read
		// on at the next look.
		next = cursor{Inode: ino}
		return split(data), next, skipped, true, nil
	}
	whole := data
	if cut := bytes.LastIndexByte(data, '\n'); cut >= 0 {
		whole = data[:cut+1]
	} else if !(eof && ended) && len(data) < readMax {
		whole = nil
	}
	if eof && ended {
		whole = data
	}
	next.Offset += int64(len(whole))
	return split(whole), next, skipped, !eof || aside, nil
}

// readAt reads a file from an offset, readMax at most, and says whether it
// read to the end.
func readAt(path string, offset int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false, err
	}
	buf := make([]byte, readMax)
	n, err := io.ReadFull(f, buf)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return buf[:n], true, nil
	}
	if err != nil {
		return nil, false, err
	}
	// A full read may have ended right at the end of the file.
	var one [1]byte
	m, _ := f.Read(one[:])
	return buf[:n], m == 0, nil
}

// split cuts what was read into lines, without their line breaks.
func split(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := strings.TrimSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// tailMax bounds what the tail of a log reads from the disk.
const tailMax = 256 << 10

// tail is the last n lines of the log of dir, the file moved aside included
// when the one there now holds fewer.
func tail(dir string, n int) []string {
	var data []byte
	for _, name := range []string{logFile, oldLogFile} {
		room := tailMax - len(data)
		if room <= 0 {
			break
		}
		chunk := lastBytes(filepath.Join(dir, name), room)
		data = append(chunk, data...)
		if bytes.Count(data, []byte{'\n'}) > n {
			break
		}
	}
	lines := split(data)
	if len(data) >= tailMax && len(lines) > 0 {
		// The first line is cut at the start of what was read.
		lines = lines[1:]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func lastBytes(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	from := max(info.Size()-int64(n), 0)
	out := make([]byte, info.Size()-from)
	m, _ := f.ReadAt(out, from)
	return out[:m]
}

// logSize is what the log of dir holds on the disk.
func logSize(dir string) int64 {
	var total int64
	for _, name := range []string{logFile, oldLogFile} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
			total += info.Size()
		}
	}
	return total
}
