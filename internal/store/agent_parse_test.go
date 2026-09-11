package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type snapParts struct {
	Mem, Disks, Net, Sessions, Probes string
}

func goodParts() snapParts {
	return snapParts{
		Mem:      `{"total": 100, "used": 40, "swapUsed": 1}`,
		Disks:    `[{"mount": "/", "total": 10, "used": 3}]`,
		Net:      `[{"name": "eth0", "rxRate": 100, "txRate": 50}]`,
		Sessions: `[{"session": "test", "pct": 10, "tokens": 5, "messages": 2, "model": "opus"}]`,
		Probes:   `{"at": 1000, "items": [{"name": "p", "kind": "tcp", "target": "t", "ok": true, "outcome": "ok"}]}`,
	}
}

func (p snapParts) json(at int64) []byte {
	return fmt.Appendf(nil, `{
		"at": %d,
		"host": {"cpuPct": 5, "load": [1.5, 1.2, 1.0], "mem": %s, "disks": %s, "net": %s},
		"sessions": %s,
		"probes": %s
	}`, at, p.Mem, p.Disks, p.Net, p.Sessions, p.Probes)
}

func parsedBlocks(s *snapshot) map[string]bool {
	return map[string]bool{
		blockHost:     s.hostOK,
		blockDisks:    len(s.disks) > 0,
		blockNet:      len(s.net) > 0,
		blockSessions: len(s.sessions) > 0,
		blockProbes:   len(s.probes.Items) > 0,
	}
}

func TestSnapshotBlocksAreParsedApart(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*snapParts)
		fault  string
	}{
		{"memory as a fractional number", func(p *snapParts) { p.Mem = `{"total": 100.5, "used": 40}` }, blockHost},
		{"disks as a string", func(p *snapParts) { p.Disks = `"none"` }, blockDisks},
		{"disk space as a fractional number", func(p *snapParts) {
			p.Disks = `[{"mount": "/", "total": 10, "used": 3.5}]`
		}, blockDisks},
		{"a network rate as a string", func(p *snapParts) { p.Net = `[{"name": "eth0", "rxRate": "a lot"}]` }, blockNet},
		{"session tokens as a fractional number", func(p *snapParts) {
			p.Sessions = `[{"session": "test", "tokens": 1.5}]`
		}, blockSessions},
		{"a probe latency as a string", func(p *snapParts) {
			p.Probes = `{"at": 1000, "items": [{"name": "p", "kind": "tcp", "latencyMs": "a long time"}]}`
		}, blockProbes},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := goodParts()
			c.break_(&p)

			s, err := parseSnapshot(p.json(1700000000))
			if err != nil {
				t.Fatalf("the snapshot was rejected entirely, yet one block was broken: %v", err)
			}
			if s.at != 1700000000 {
				t.Errorf("the snapshot's time is lost: %d", s.at)
			}

			if len(s.faults) != 1 {
				t.Fatalf("wanted one failed block, got %d: %+v", len(s.faults), s.faults)
			}
			if got := s.faults[0].Block; got != c.fault {
				t.Errorf("%q failed, yet %q was the broken one", got, c.fault)
			}
			if s.faults[0].Error == "" {
				t.Error("a failure without an error text: there will be nothing to show in the panel")
			}

			for block, ok := range parsedBlocks(s) {
				if block == c.fault {
					if ok {
						t.Errorf("block %q did not parse, yet it stayed half filled in", block)
					}
					continue
				}
				if !ok {
					t.Errorf("block %q went down together with %q, although there is nothing wrong with it", block, c.fault)
				}
			}
		})
	}
}

func TestSnapshotWholeParsesClean(t *testing.T) {
	s, err := parseSnapshot(goodParts().json(1700000000))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.faults) != 0 {
		t.Fatalf("an intact snapshot produced failures: %+v", s.faults)
	}
	for block, ok := range parsedBlocks(s) {
		if !ok {
			t.Errorf("block %q did not parse", block)
		}
	}
	if s.empty() {
		t.Error("the snapshot is considered empty")
	}
}

func TestSnapshotMissingBlocksAreNotFaults(t *testing.T) {
	s, err := parseSnapshot([]byte(`{"at": 1700000000, "host": {"cpuPct": 1, "mem": {"total": 2, "used": 1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.faults) != 0 {
		t.Fatalf("absent blocks were counted as a failure: %+v", s.faults)
	}
	if !s.hostOK {
		t.Error("the host core did not parse")
	}
}

func TestSnapshotRejectedWhole(t *testing.T) {
	if _, err := parseSnapshot([]byte(`not json`)); err == nil {
		t.Error("rubbish was taken for a snapshot")
	}
	if _, err := parseSnapshot([]byte(`{"at": "yesterday"}`)); err == nil {
		t.Error("a snapshot with a non-numeric time was accepted")
	}
	s, err := parseSnapshot([]byte(`{"host": {"cpuPct": 1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.at != 0 {
		t.Error("the time came out of nowhere")
	}
}

func TestWriterSnapshotFaults(t *testing.T) {
	w := NewWriter(nil, "test")

	broken := goodParts()
	broken.Disks = `[{"mount": "/", "total": 10, "used": 3.5}]`

	w.AgentSnapshot(broken.json(1700000000))
	faults := w.SnapshotFaults()
	if len(faults) != 1 || faults[0].Block != blockDisks {
		t.Fatalf("wanted a failure on the disks, got %+v", faults)
	}
	if faults[0].Count != 1 {
		t.Errorf("a failure count of %d, wanted 1", faults[0].Count)
	}
	if faults[0].Since.IsZero() || faults[0].Last.IsZero() {
		t.Error("a failure without a time: in the panel it will be unclear how long the hole has been there")
	}
	if !strings.Contains(faults[0].Error, "3.5") {
		t.Errorf("the failure's text does not carry the value itself: %q", faults[0].Error)
	}

	w.AgentSnapshot(broken.json(1700000000))
	if got := w.SnapshotFaults()[0].Count; got != 1 {
		t.Errorf("a repeat of the same snapshot wound the count up to %d", got)
	}

	since := faults[0].Since
	w.AgentSnapshot(broken.json(1700000010))
	again := w.SnapshotFaults()
	if again[0].Count != 2 {
		t.Errorf("a second snapshot with the same trouble gave a count of %d, wanted 2", again[0].Count)
	}
	if !again[0].Since.Equal(since) {
		t.Error("the failure's start moved: it is unclear from what moment the hole dates")
	}

	w.AgentSnapshot(goodParts().json(1700000020))
	if got := w.SnapshotFaults(); len(got) != 0 {
		t.Errorf("a block that mended itself stayed among the failed ones: %+v", got)
	}
}

func TestSnapshotAllBlocksBrokenIsNotQueued(t *testing.T) {
	w := NewWriter(nil, "test")
	w.AgentSnapshot([]byte(`{"at": 1700000000, "host": "down", "sessions": 1, "probes": 2}`))

	if n := w.Queued(); n != 0 {
		t.Errorf("%d batches joined the queue without a single parsed block", n)
	}
	if got := w.SnapshotFaults(); len(got) == 0 {
		t.Fatal("not a single failure was noted — the panel will stay quiet")
	}
}

func TestSnapshotDuplicateNotQueued(t *testing.T) {
	w := NewWriter(nil, "test")
	raw := goodParts().json(time.Now().Unix())

	w.AgentSnapshot(raw)
	w.AgentSnapshot(raw)
	if n := w.Queued(); n != 1 {
		t.Errorf("%d batches in the queue, wanted one", n)
	}
}
