package usage

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/store"
)

// Scanner is the single collection round of the service.
type Scanner struct {
	link *Client
	db   Store

	mu    sync.Mutex
	state State
}

// Store is what a collection round needs from the storage.
type Store interface {
	UsageScanPoints(ctx context.Context) (map[string]store.UsageScanPoint, error)
	MarkUsageScanMissing(ctx context.Context, paths []string) (int, error)
	WriteUsageFile(ctx context.Context, f store.UsageFile) error
}

// State is what to show a person while a scan runs or does not.
type State struct {
	Running    bool              `json:"running"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Error      string            `json:"error,omitempty"`
	Contours   []ContourProgress `json:"contours"`
	Failed     int               `json:"failed"`
	Missing    int               `json:"missing"`
	Skipped    int               `json:"skipped"`
}

// ContourProgress is how much is ahead and how much is done in one contour.
type ContourProgress struct {
	Contour   string `json:"contour"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	FilesDone int    `json:"filesDone"`
	BytesDone int64  `json:"bytesDone"`
}

// Estimate is the price of a scan before it starts.
type Estimate struct {
	Contours     []ContourProgress `json:"contours"`
	Files        int               `json:"files"`
	Bytes        int64             `json:"bytes"`
	PendingFiles int               `json:"pendingFiles"`
	PendingBytes int64             `json:"pendingBytes"`
}

func NewScanner(link *Client, db Store) *Scanner {
	return &Scanner{link: link, db: db}
}

// ErrBusy means a round is already running.
var ErrBusy = errors.New("usage collection is already running")

// Available reports whether there is anyone to ask and anywhere to write.
func (s *Scanner) Available() bool {
	return s != nil && s.link.Available() && s.db != nil && !isNilStore(s.db)
}

func isNilStore(db Store) bool {
	v := reflect.ValueOf(db)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// Ping reports whether the agent answers and with how many parser processes.
func (s *Scanner) Ping(ctx context.Context) (Pong, error) {
	if !s.Available() {
		return Pong{}, ErrNoAgent
	}
	return s.link.Ping(ctx)
}

// State returns a snapshot of the progress.
func (s *Scanner) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Contours = slices.Clone(s.state.Contours)
	return st
}

// Estimate returns the price of a scan without starting it.
func (s *Scanner) Estimate(ctx context.Context) (Estimate, error) {
	files, err := s.link.List(ctx)
	if err != nil {
		return Estimate{}, err
	}
	points, err := s.db.UsageScanPoints(ctx)
	if err != nil {
		return Estimate{}, err
	}
	p := buildPlan(files, points)

	est := Estimate{PendingFiles: len(p.tasks), Contours: p.progress()}
	byContour := map[string]*ContourProgress{}
	for i := range est.Contours {
		byContour[est.Contours[i].Contour] = &est.Contours[i]
		est.Contours[i].Files, est.Contours[i].Bytes = 0, 0
	}
	for _, f := range files {
		c, ok := byContour[f.Contour]
		if !ok {
			est.Contours = append(est.Contours, ContourProgress{Contour: f.Contour})
			c = &est.Contours[len(est.Contours)-1]
			byContour[f.Contour] = c
		}
		c.Files++
		c.Bytes += f.Size
		est.Files++
		est.Bytes += f.Size
	}
	for _, t := range p.tasks {
		est.PendingBytes += p.read[t.Path]
	}
	return est, nil
}

// Run performs a whole collection round.
func (s *Scanner) Run(ctx context.Context) error {
	if !s.Available() {
		return ErrNoAgent
	}
	s.mu.Lock()
	if s.state.Running {
		s.mu.Unlock()
		return ErrBusy
	}
	now := time.Now()
	s.state = State{Running: true, StartedAt: &now}
	s.mu.Unlock()

	err := s.run(ctx)

	s.mu.Lock()
	done := time.Now()
	s.state.Running = false
	s.state.FinishedAt = &done
	if err != nil {
		s.state.Error = err.Error()
	}
	s.mu.Unlock()
	return err
}

func (s *Scanner) run(ctx context.Context) error {
	files, err := s.link.List(ctx)
	if err != nil {
		return err
	}
	points, err := s.db.UsageScanPoints(ctx)
	if err != nil {
		return err
	}
	p := buildPlan(files, points)

	s.mu.Lock()
	s.state.Contours = p.progress()
	s.state.Skipped = p.skipped
	s.mu.Unlock()

	if len(p.missing) > 0 {
		n, err := s.db.MarkUsageScanMissing(ctx, p.missing)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.state.Missing = n
		s.mu.Unlock()
	}

	var redo []Task
	err = s.link.Parse(ctx, p.tasks, func(res Result) error {
		if res.Rewind && res.Error == "" {
			redo = append(redo, Task{Path: res.Path})
			return nil
		}
		return s.write(ctx, p, res, p.offsets[res.Path])
	})
	if err != nil || len(redo) == 0 {
		return err
	}
	return s.link.Parse(ctx, redo, func(res Result) error {
		return s.write(ctx, p, res, 0)
	})
}

func (s *Scanner) write(ctx context.Context, p plan, res Result, offset int64) error {
	f, ok := p.files[res.Path]
	if !ok {
		return fmt.Errorf("the agent answered about a file that was not requested: %q", res.Path)
	}
	if res.Error != "" {
		s.failed()
		return nil
	}
	file, err := toStore(res, f, offset)
	if err != nil {
		s.failed()
		return nil
	}
	if err := s.db.WriteUsageFile(ctx, file); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Contours {
		if s.state.Contours[i].Contour == f.Contour {
			s.state.Contours[i].FilesDone++
			s.state.Contours[i].BytesDone += p.read[res.Path]
			break
		}
	}
	return nil
}

func (s *Scanner) failed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Failed++
}

type plan struct {
	tasks   []Task
	files   map[string]File
	offsets map[string]int64
	read    map[string]int64
	missing []string
	skipped int
}

func (p plan) progress() []ContourProgress {
	out := []ContourProgress{}
	idx := map[string]int{}
	for _, t := range p.tasks {
		f := p.files[t.Path]
		i, ok := idx[f.Contour]
		if !ok {
			i = len(out)
			idx[f.Contour] = i
			out = append(out, ContourProgress{Contour: f.Contour})
		}
		out[i].Files++
		out[i].Bytes += p.read[t.Path]
	}
	return out
}

func buildPlan(files []File, points map[string]store.UsageScanPoint) plan {
	p := plan{
		files:   make(map[string]File, len(files)),
		offsets: make(map[string]int64, len(files)),
		read:    make(map[string]int64, len(files)),
	}
	seen := make(map[string]bool, len(files))

	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(a, b File) int {
		switch {
		case a.First == "" && b.First == "":
		case a.First == "":
			return 1
		case b.First == "":
			return -1
		case a.First != b.First:
			return strings.Compare(a.First, b.First)
		}
		return strings.Compare(a.Path, b.Path)
	})

	for _, f := range ordered {
		seen[f.Path] = true
		p.files[f.Path] = f

		point, known := points[f.Path]
		offset := int64(0)
		if known && headEqual(point.HeadSum, f.Head) && f.Size >= point.Size {
			offset = min(point.Offset, f.Size)
			if f.Size == point.Size && f.Inode == point.Inode && point.MissingAt.IsZero() {
				p.skipped++
				continue
			}
		}
		p.tasks = append(p.tasks, Task{Path: f.Path, Offset: offset})
		p.offsets[f.Path] = offset
		p.read[f.Path] = f.Size - offset
	}

	for path, point := range points {
		if !seen[path] && point.MissingAt.IsZero() {
			p.missing = append(p.missing, path)
		}
	}
	slices.Sort(p.missing)
	return p
}

func headEqual(stored []byte, head string) bool {
	if len(stored) == 0 || head == "" {
		return false
	}
	return hex.EncodeToString(stored) == head
}

func toStore(res Result, f File, offset int64) (store.UsageFile, error) {
	out := store.UsageFile{
		Session: store.UsageSession{
			SessionID: res.Session.SessionID,
			Contour:   res.Session.Contour,
			CWD:       res.Session.CWD,
			GitBranch: res.Session.GitBranch,
			Version:   res.Session.Version,
		},
		Rewrite: offset == 0,
		Agents:  res.Session.Agents,
	}
	if res.Session.Contour == "" {
		out.Session.Contour = f.Contour
	}
	var err error
	if out.Session.StartedAt, err = parseTime(res.Session.StartedAt); err != nil {
		return out, err
	}
	if out.Session.EndedAt, err = parseTime(res.Session.EndedAt); err != nil {
		return out, err
	}
	if out.Since, err = parseTime(res.Since); err != nil {
		return out, err
	}
	for _, e := range res.Events {
		bucket, err := parseTime(e.Bucket)
		if err != nil || bucket.IsZero() {
			return out, fmt.Errorf("the event hour cannot be parsed: %q", e.Bucket)
		}
		out.Events = append(out.Events, store.UsageEventRow{
			Bucket: bucket, Agent: e.Agent,
			Messages: e.Messages, Compacts: e.Compacts,
			Interrupts: e.Interrupts, InterruptsTool: e.InterruptsTool,
			APIErrors: e.APIErrors,
			Idle: store.IdlePauses{
				Under30s: e.Idle.Under30s, Under2m: e.Idle.Under2m,
				Under10m: e.Idle.Under10m, Under1h: e.Idle.Under1h,
				Under4h: e.Idle.Under4h, Over4h: e.Idle.Over4h,
				MaxMS: e.Idle.MaxMS,
			},
		})
	}
	for _, r := range res.Rows {
		bucket, err := parseTime(r.Bucket)
		if err != nil || bucket.IsZero() {
			return out, fmt.Errorf("the row hour cannot be parsed: %q", r.Bucket)
		}
		out.Rows = append(out.Rows, store.UsageRow{
			Bucket: bucket, Model: r.Model, Agent: r.Agent,
			Speed: r.Speed, ServiceTier: r.ServiceTier, AgentKind: r.AgentKind,
			Answers: r.Answers, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
			CacheRead: r.CacheRead, CacheCreation: r.CacheCreation,
			Cache1h: r.Cache1h, Cache5m: r.Cache5m,
			Iterations: r.Iterations, Thinking: r.Thinking,
			LatencySumMS: r.LatencySumMS, LatencyMaxMS: r.LatencyMaxMS,
		})
	}
	for _, t := range res.Tools {
		bucket, err := parseTime(t.Bucket)
		if err != nil || bucket.IsZero() {
			return out, fmt.Errorf("the tool hour cannot be parsed: %q", t.Bucket)
		}
		out.Tools = append(out.Tools, store.UsageToolRow{
			Bucket: bucket, Agent: t.Agent, Tool: t.Tool, Calls: t.Calls, Errors: t.Errors,
		})
	}

	head, err := hex.DecodeString(f.Head)
	if err != nil {
		head = nil
	}
	newOffset := max(res.Offset, offset)
	out.Scan = store.UsageScanPoint{
		Path: f.Path, Contour: f.Contour, SessionID: res.Session.SessionID,
		Inode: f.Inode, Size: f.Size, Offset: newOffset, HeadSum: head,
	}
	return out, nil
}

func parseTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("the timestamp %q cannot be parsed: %w", v, err)
	}
	return t.UTC(), nil
}

const daily = 24 * time.Hour

const firstDelay = 5 * time.Minute

// RunDaily is the background top-up, which shows no progress.
func (s *Scanner) RunDaily(ctx context.Context) {
	if !s.Available() {
		return
	}
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := s.Run(ctx); err != nil && !errors.Is(err, ErrBusy) && ctx.Err() == nil {
			log.Printf("usage: the background scan did not go through: %v", err)
		}
		timer.Reset(daily)
	}
}
