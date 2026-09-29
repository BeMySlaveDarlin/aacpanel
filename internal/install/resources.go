package install

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// gb is a gigabyte the way a machine is sold: a VM of 2 GB reports a
// little under 2 GiB of memory, and should not fall below a floor of 2.
const gb = 1_000_000_000

// The room the install needs: the images and the build under docker's
// root, the state of the panel, and memory for the image build.
const (
	dockerStop = 3 * gb
	dockerWarn = 5 * gb
	stateStop  = 1 * gb
	memoryStop = 2 * gb
	memoryWarn = 4 * gb
)

func gigabytes(n uint64) string {
	return strconv.FormatFloat(float64(n)/gb, 'f', 1, 64)
}

// room checks free space and memory.
func (c *checker) room() {
	var parts []string
	stops := len(c.out)
	if c.dockerDir != "" {
		if sp, err := c.m.Space(c.dockerDir); err == nil {
			switch {
			case sp.Free < dockerStop:
				c.add(Stop, "stop: %s has %s GB free; the images and the build need about %d GB.",
					c.dockerDir, gigabytes(sp.Free), dockerStop/gb)
			case sp.Free < dockerWarn:
				c.add(Warn, "warn: %s has %s GB free: the images and the build fit, with little room left to grow.",
					c.dockerDir, gigabytes(sp.Free))
			}
			parts = append(parts, fmt.Sprintf("%s GB free under %s", gigabytes(sp.Free), c.dockerDir))
		}
	}
	if dir := c.existing(c.f.StateDir); dir != "" {
		if sp, err := c.m.Space(dir); err == nil && sp.Free < stateStop {
			c.add(Stop, "stop: %s has %s GB free; the state of the panel needs about %d GB.",
				dir, gigabytes(sp.Free), stateStop/gb)
		}
	}
	if raw, err := c.m.ReadFile("/proc/meminfo"); err == nil {
		if total := memTotal(string(raw)); total > 0 {
			switch {
			case total < memoryStop:
				c.add(Stop, "stop: %s GB of memory: the image build does not fit.", gigabytes(total))
			case total < memoryWarn:
				c.add(Warn, "warn: %s GB of memory: the image build may run short of it.", gigabytes(total))
			}
			parts = append(parts, gigabytes(total)+" GB of memory")
		}
	}
	if !c.stoppedSince(stops) && len(parts) > 0 {
		c.insert(stops, Finding{Mark: Pass, Text: strings.Join(parts, " · ")})
	}
}

// existing is the path itself or its nearest parent that is there: the
// state directory is made later, on the file system of its parent.
func (c *checker) existing(path string) string {
	for p := path; ; p = filepath.Dir(p) {
		if _, err := c.m.Stat(p); err == nil {
			return p
		}
		if p == "/" || p == "." {
			return ""
		}
	}
}

func memTotal(meminfo string) uint64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			f := strings.Fields(rest)
			if len(f) > 0 {
				kb, _ := strconv.ParseUint(f[0], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

// network checks the two places the install downloads from: the images and
// the modules of the executor. A silence is a warning, not a stop: a mirror
// or a proxy may still get the downloads through.
func (c *checker) network() {
	type probe struct {
		host, url, warn string
		ok              func(int) bool
		answered        bool
	}
	probes := []*probe{
		{host: "registry-1.docker.io", url: "https://registry-1.docker.io/v2/",
			warn: "warn: registry-1.docker.io does not answer; pulling the images may fail.",
			ok:   func(s int) bool { return s == 200 || s == 401 }},
		{host: "proxy.golang.org", url: "https://proxy.golang.org/",
			warn: "warn: proxy.golang.org does not answer; building the executor may fail.",
			ok:   func(s int) bool { return s > 0 && s < 500 }},
	}
	var wg sync.WaitGroup
	for _, p := range probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := c.m.Reach(p.url)
			p.answered = err == nil && p.ok(status)
		}()
	}
	wg.Wait()
	var fine []string
	for _, p := range probes {
		if p.answered {
			fine = append(fine, p.host)
		} else {
			c.add(Warn, "%s", p.warn)
		}
	}
	if len(fine) > 0 {
		verb := "answers"
		if len(fine) > 1 {
			verb = "answer"
		}
		c.add(Pass, "%s %s", strings.Join(fine, " and "), verb)
	}
}

// older tells whether version a is older than b, comparing the numbers of
// each at the head of the string: "2.24.6+ds1-0ubuntu2" is 2.24.6, and an
// epoch like "1:" is dropped.
func older(a, b string) bool {
	x, y := numbers(a), numbers(b)
	for i := 0; i < max(len(x), len(y)); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p < q
		}
	}
	return false
}

func numbers(v string) []int {
	if _, rest, ok := strings.Cut(v, ":"); ok {
		v = rest
	}
	v = strings.TrimPrefix(v, "v")
	end := strings.IndexFunc(v, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if end >= 0 {
		v = v[:end]
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
