package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client is a thin client to the Docker Engine API.
type Client struct {
	base string
	http *http.Client
}

func New(host string) *Client {
	base := host
	tr := &http.Transport{}
	if strings.HasPrefix(host, "unix://") {
		sock := strings.TrimPrefix(host, "unix://")
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}
		base = "http://docker"
	}
	return &Client{
		base: strings.TrimSuffix(base, "/"),
		http: &http.Client{Transport: tr},
	}
}

func (d *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("docker api %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

type apiContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []apiPort         `json:"Ports"`
}

type apiPort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type Container struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Service string   `json:"service"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Health  string   `json:"health"`
	Ports   []string `json:"ports"`
	Created int64    `json:"created"`

	CPU      float64 `json:"cpu"`
	Mem      uint64  `json:"mem"`
	MemLimit uint64  `json:"memLimit"`
	MemPct   float64 `json:"memPct"`
	HasStats bool    `json:"hasStats"`

	Disk      int64 `json:"disk"`
	DiskTotal int64 `json:"diskTotal"`
	HasDisk   bool  `json:"hasDisk"`
}

// Stats is a load snapshot of one container.
type Stats struct {
	CPU      float64
	Mem      uint64
	MemLimit uint64
}

// Metrics is what the background collector gathers and mixes into the tree.
type Metrics struct {
	Stats map[string]Stats
	Disk  map[string]Disk
}

// Disk is how much a container takes on disk.
type Disk struct {
	Rw     int64
	RootFs int64
}

type apiStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

// Stats measures the load of a container.
func (d *Client) Stats(ctx context.Context, id string) (*Stats, error) {
	resp, err := d.get(ctx, "/containers/"+url.PathEscape(id)+"/stats?stream=false&one-shot=false")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw apiStats
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	s := &Stats{MemLimit: raw.MemoryStats.Limit}

	if raw.MemoryStats.Usage > raw.MemoryStats.Stats.InactiveFile {
		s.Mem = raw.MemoryStats.Usage - raw.MemoryStats.Stats.InactiveFile
	} else {
		s.Mem = raw.MemoryStats.Usage
	}

	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemCPUUsage) - float64(raw.PreCPUStats.SystemCPUUsage)
	cpus := raw.CPUStats.OnlineCPUs
	if cpus == 0 {
		cpus = 1
	}
	if cpuDelta > 0 && sysDelta > 0 {
		s.CPU = cpuDelta / sysDelta * float64(cpus) * 100
	}
	return s, nil
}

type Stack struct {
	Name       string      `json:"name"`
	Running    int         `json:"running"`
	Total      int         `json:"total"`
	Containers []Container `json:"containers"`

	CPU      float64 `json:"cpu"`
	Mem      uint64  `json:"mem"`
	HasStats bool    `json:"hasStats"`
	Disk     int64   `json:"disk"`
	HasDisk  bool    `json:"hasDisk"`
}

type Tree struct {
	Stacks  []Stack `json:"stacks"`
	Running int     `json:"running"`
	Total   int     `json:"total"`
	At      int64   `json:"at"`
}

const noStack = "no stack"

// Sizes returns the on-disk sizes of the containers.
func (d *Client) Sizes(ctx context.Context) (map[string]Disk, error) {
	resp, err := d.get(ctx, "/containers/json?all=1&size=1")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []struct {
		ID         string `json:"Id"`
		SizeRw     int64  `json:"SizeRw"`
		SizeRootFs int64  `json:"SizeRootFs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make(map[string]Disk, len(raw))
	for _, c := range raw {
		out[c.ID] = Disk{Rw: c.SizeRw, RootFs: c.SizeRootFs}
	}
	return out, nil
}

// Tree builds the container tree from the listing and the given metrics.
func (d *Client) Tree(ctx context.Context, m *Metrics) (*Tree, error) {
	resp, err := d.get(ctx, "/containers/json?all=1")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []apiContainer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing the container list: %w", err)
	}

	byStack := map[string][]Container{}
	tree := &Tree{At: time.Now().Unix()}
	for _, c := range raw {
		stack := c.Labels["com.docker.compose.project"]
		if stack == "" {
			stack = noStack
		}
		cont := Container{
			ID:      c.ID,
			Name:    strings.TrimPrefix(firstName(c.Names), "/"),
			Service: c.Labels["com.docker.compose.service"],
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Health:  health(c.Status),
			Ports:   formatPorts(c.Ports),
			Created: c.Created,
		}
		if m != nil {
			if s, ok := m.Stats[c.ID]; ok && c.State == "running" {
				cont.CPU, cont.Mem, cont.MemLimit, cont.HasStats = s.CPU, s.Mem, s.MemLimit, true
			}
			if d, ok := m.Disk[c.ID]; ok {
				cont.Disk, cont.DiskTotal, cont.HasDisk = d.Rw, d.RootFs, true
			}
		}
		byStack[stack] = append(byStack[stack], cont)
		tree.Total++
		if c.State == "running" {
			tree.Running++
		}
	}

	var hostMem uint64
	for _, list := range byStack {
		for _, c := range list {
			if c.MemLimit > hostMem {
				hostMem = c.MemLimit
			}
		}
	}
	for _, list := range byStack {
		for i := range list {
			c := &list[i]
			if c.HasStats && c.MemLimit > 0 && c.MemLimit < hostMem*95/100 {
				c.MemPct = float64(c.Mem) / float64(c.MemLimit) * 100
			}
		}
	}

	for name, list := range byStack {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		s := Stack{Name: name, Containers: list, Total: len(list)}
		for _, c := range list {
			if c.State == "running" {
				s.Running++
			}
			if c.HasStats {
				s.CPU += c.CPU
				s.Mem += c.Mem
				s.HasStats = true
			}
			if c.HasDisk {
				s.Disk += c.Disk
				s.HasDisk = true
			}
		}
		tree.Stacks = append(tree.Stacks, s)
	}

	sort.Slice(tree.Stacks, func(i, j int) bool {
		a, b := tree.Stacks[i], tree.Stacks[j]
		if (a.Name == noStack) != (b.Name == noStack) {
			return b.Name == noStack
		}
		if (a.Running > 0) != (b.Running > 0) {
			return a.Running > 0
		}
		return a.Name < b.Name
	})
	return tree, nil
}

func firstName(names []string) string {
	if len(names) == 0 {
		return "<unnamed>"
	}
	return names[0]
}

func health(status string) string {
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	}
	return ""
}

func formatPorts(ports []apiPort) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range ports {
		var s string
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

const eventFilter = `{"type":["container"],"event":["create","start","stop","die","kill","destroy",` +
	`"pause","unpause","restart","rename","update","health_status"]}`

// Events streams container events and calls onEvent for each significant one.
func (d *Client) Events(ctx context.Context, onEvent func()) error {
	resp, err := d.get(ctx, "/events?filters="+url.QueryEscape(eventFilter))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var ev struct {
			Action string `json:"Action"`
		}
		if err := dec.Decode(&ev); err != nil {
			return err
		}
		if strings.HasPrefix(ev.Action, "exec_") {
			continue
		}
		onEvent()
	}
}

// Logs streams the container log line by line into out.
func (d *Client) Logs(ctx context.Context, id string, tail int, out func(stream, line string)) error {
	q := url.Values{
		"stdout":     {"1"},
		"stderr":     {"1"},
		"follow":     {"1"},
		"timestamps": {"1"},
		"tail":       {fmt.Sprint(tail)},
	}
	resp, err := d.get(ctx, "/containers/"+url.PathEscape(id)+"/logs?"+q.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "application/vnd.docker.multiplexed-stream" {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			out("stdout", sc.Text())
		}
		return sc.Err()
	}

	br := bufio.NewReaderSize(resp.Body, 64*1024)
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		if size == 0 {
			continue
		}
		take := size
		if take > 1<<20 {
			take = 1 << 20
		}
		buf := make([]byte, take)
		if _, err := io.ReadFull(br, buf); err != nil {
			return err
		}
		if rest := int64(size - take); rest > 0 {
			if _, err := io.CopyN(io.Discard, br, rest); err != nil {
				return err
			}
		}
		stream := "stdout"
		if hdr[0] == 2 {
			stream = "stderr"
		}
		for _, line := range strings.Split(strings.TrimRight(string(buf), "\n"), "\n") {
			out(stream, line)
		}
	}
}
