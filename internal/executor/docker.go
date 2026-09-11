// Package executor performs the allowed actions on the host.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	labelProject   = "com.docker.compose.project"
	labelService   = "com.docker.compose.service"
	labelDependsOn = "com.docker.compose.depends_on"
)

const stopTimeout = 30

// Docker is a Docker Engine API client for state-changing calls.
type Docker struct {
	base string
	http *http.Client
}

// NewDocker creates a client for the given daemon host.
func NewDocker(host string) *Docker {
	base := host
	tr := &http.Transport{}
	if sock, ok := strings.CutPrefix(host, "unix://"); ok {
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}
		base = "http://docker"
	}
	return &Docker{
		base: strings.TrimSuffix(base, "/"),
		http: &http.Client{Transport: tr, Timeout: 2 * time.Minute},
	}
}

// Container describes a container the panel can manage.
type Container struct {
	ID        string
	Name      string
	Project   string
	Service   string
	DependsOn []string
	State     string
}

// Running reports whether the container is running.
func (c Container) Running() bool { return c.State == "running" }

type apiContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// List returns every container, including stopped ones.
func (d *Docker) List(ctx context.Context) ([]Container, error) {
	resp, err := d.do(ctx, http.MethodGet, "/containers/json?all=1")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []apiContainer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}

	out := make([]Container, 0, len(raw))
	for _, c := range raw {
		out = append(out, Container{
			ID:        c.ID,
			Name:      name(c.Names),
			Project:   c.Labels[labelProject],
			Service:   c.Labels[labelService],
			DependsOn: dependsOn(c.Labels[labelDependsOn]),
			State:     c.State,
		})
	}
	return out, nil
}

// Start starts the container with the given id.
func (d *Docker) Start(ctx context.Context, id string) error {
	return d.post(ctx, "/containers/"+id+"/start")
}

// Stop stops the container with the given id.
func (d *Docker) Stop(ctx context.Context, id string) error {
	return d.post(ctx, fmt.Sprintf("/containers/%s/stop?t=%d", id, stopTimeout))
}

// Restart restarts the container with the given id.
func (d *Docker) Restart(ctx context.Context, id string) error {
	return d.post(ctx, fmt.Sprintf("/containers/%s/restart?t=%d", id, stopTimeout))
}

func (d *Docker) post(ctx context.Context, path string) error {
	resp, err := d.do(ctx, http.MethodPost, path)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (d *Docker) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker is unavailable: %w", err)
	}
	if resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("docker answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func name(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func dependsOn(label string) []string {
	if label == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(label, ",") {
		service, _, _ := strings.Cut(strings.TrimSpace(part), ":")
		if service != "" {
			out = append(out, service)
		}
	}
	return out
}
