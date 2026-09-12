package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	blockHost     = "host"
	blockDisks    = "disks"
	blockNet      = "net"
	blockSessions = "sessions"
	blockProbes   = "probes"
)

// SnapshotBlocks lists every block of the snapshot.
var SnapshotBlocks = []string{
	blockHost, blockDisks, blockNet, blockSessions, blockProbes,
}

type hostCore struct {
	CPUPct float64   `json:"cpuPct"`
	Load   []float64 `json:"load"`
	Mem    struct {
		Total    int64 `json:"total"`
		Used     int64 `json:"used"`
		SwapUsed int64 `json:"swapUsed"`
	} `json:"mem"`

	// A temperature is a pointer because its absence is a fact of the machine,
	// not a zero: the collector leaves the key out where there is no sensor,
	// and the history keeps a null there. The disks are several, and the
	// history keeps one number for them, the hottest: a rule and a chart ask
	// whether any disk is cooking, while the screen names each one from the
	// live snapshot, so a partitioned family of tables per device would serve
	// a series nobody draws.
	CPUTemp  *float64 `json:"cpuTemp"`
	MemTemp  *float64 `json:"memTemp"`
	DiskTemp *float64 `json:"diskTemp"`
}

type diskRow struct {
	Mount string `json:"mount"`
	Total int64  `json:"total"`
	Used  int64  `json:"used"`
}

type netRow struct {
	Name    string `json:"name"`
	RxRate  int64  `json:"rxRate"`
	TxRate  int64  `json:"txRate"`
	RxTotal *int64 `json:"rx"`
	TxTotal *int64 `json:"tx"`
}

type sessionRow struct {
	Session   string  `json:"session"`
	Pct       float64 `json:"pct"`
	Tokens    int64   `json:"tokens"`
	Messages  int     `json:"messages"`
	Model     string  `json:"model"`
	SessionID string  `json:"sessionId"`
	CWD       string  `json:"cwd"`
}

type probesBlock struct {
	At    int64 `json:"at"`
	Items []struct {
		Name      string  `json:"name"`
		Kind      string  `json:"kind"`
		Target    string  `json:"target"`
		Interval  *int    `json:"intervalSec"`
		Timeout   *int    `json:"timeoutSec"`
		OK        bool    `json:"ok"`
		Outcome   string  `json:"outcome"`
		LatencyMS *int    `json:"latencyMs"`
		Error     *string `json:"error"`
	} `json:"items"`
}

type snapshot struct {
	at       int64
	host     hostCore
	hostOK   bool
	disks    []diskRow
	net      []netRow
	sessions []sessionRow
	probes   probesBlock

	faults []SnapshotFault
}

// SnapshotFault is a block of the snapshot that failed to parse.
type SnapshotFault struct {
	Block string    `json:"block"`
	Error string    `json:"error"`
	Since time.Time `json:"since"`
	Last  time.Time `json:"last"`
	Count int       `json:"count"`
}

func (s *snapshot) fault(block string, err error) {
	s.faults = append(s.faults, SnapshotFault{Block: block, Error: err.Error()})
}

func (s *snapshot) empty() bool {
	return !s.hostOK && len(s.disks) == 0 && len(s.net) == 0 &&
		len(s.sessions) == 0 && len(s.probes.Items) == 0
}

func take[T any](s *snapshot, block string, raw json.RawMessage, dst *T) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		s.fault(block, err)
		var zero T
		*dst = zero
	}
}

func parseSnapshot(raw []byte) (*snapshot, error) {
	var top struct {
		At       int64           `json:"at"`
		Host     json.RawMessage `json:"host"`
		Sessions json.RawMessage `json:"sessions"`
		Probes   json.RawMessage `json:"probes"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}

	s := &snapshot{at: top.At}

	if len(top.Host) > 0 && !bytes.Equal(top.Host, []byte("null")) {
		var core hostCore
		if err := json.Unmarshal(top.Host, &core); err != nil {
			s.fault(blockHost, err)
		} else {
			s.host, s.hostOK = core, true
		}

		var lists struct {
			Disks json.RawMessage `json:"disks"`
			Net   json.RawMessage `json:"net"`
		}
		if err := json.Unmarshal(top.Host, &lists); err != nil {
			if s.hostOK {
				s.fault(blockHost, err)
				s.hostOK = false
			}
		} else {
			take(s, blockDisks, lists.Disks, &s.disks)
			take(s, blockNet, lists.Net, &s.net)
		}
	}

	take(s, blockSessions, top.Sessions, &s.sessions)
	take(s, blockProbes, top.Probes, &s.probes)

	return s, nil
}

// AgentSnapshot queues the agent's snapshot.
func (w *Writer) AgentSnapshot(raw []byte) {
	if w == nil || len(raw) == 0 {
		return
	}
	s, err := parseSnapshot(raw)
	if err != nil {
		w.gripe(fmt.Errorf("the agent snapshot does not parse: %w", err), 0)
		return
	}
	if s.at == 0 {
		return
	}

	w.mu.Lock()
	dup := s.at == w.lastAgentAt
	if !dup {
		w.lastAgentAt = s.at
		w.noteFaults(s.faults, time.Now())
	}
	w.mu.Unlock()
	if dup {
		return
	}

	if s.empty() {
		return
	}
	w.enqueue(&agentBatch{ts: time.Unix(s.at, 0), snap: s})
}

func (w *Writer) noteFaults(list []SnapshotFault, now time.Time) {
	if len(list) == 0 {
		w.faults = nil
		return
	}
	next := make(map[string]SnapshotFault, len(list))
	for _, f := range list {
		f.Since, f.Count = now, 1
		if prev, ok := w.faults[f.Block]; ok {
			f.Since, f.Count = prev.Since, prev.Count+1
		}
		f.Last = now
		next[f.Block] = f
	}
	w.faults = next
}

// SnapshotFaults returns the blocks of the agent's last snapshot that failed to parse.
func (w *Writer) SnapshotFaults() []SnapshotFault {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.faults) == 0 {
		return nil
	}
	out := make([]SnapshotFault, 0, len(w.faults))
	for _, f := range w.faults {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Block < out[j].Block })
	return out
}

type agentBatch struct {
	ts   time.Time
	snap *snapshot
}

func (b *agentBatch) rows() int {
	n := len(b.snap.disks) + len(b.snap.net) +
		len(b.snap.sessions) + len(b.snap.probes.Items)
	if b.snap.hostOK {
		n++
	}
	return n
}

func (b *agentBatch) write(ctx context.Context, tx pgx.Tx, hostID int) error {
	s := b.snap
	batch := &pgx.Batch{}

	if s.hostOK {
		var load1 *float64
		if len(s.host.Load) > 0 {
			load1 = &s.host.Load[0]
		}
		batch.Queue(`
			INSERT INTO metrics_host_raw
				(ts, host_id, cpu_pct, load1, mem_used, mem_total, swap_used, cpu_temp, mem_temp, disk_temp)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT DO NOTHING`,
			b.ts, hostID, s.host.CPUPct, load1, s.host.Mem.Used, s.host.Mem.Total, s.host.Mem.SwapUsed,
			s.host.CPUTemp, s.host.MemTemp, s.host.DiskTemp)
	}

	for _, d := range s.disks {
		batch.Queue(`
			INSERT INTO metrics_disk_raw (ts, host_id, mount, used, total)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`,
			b.ts, hostID, d.Mount, d.Used, d.Total)
	}

	for _, n := range s.net {
		batch.Queue(`
			INSERT INTO metrics_net_raw (ts, host_id, iface, rx_rate, tx_rate, rx_total, tx_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING`,
			b.ts, hostID, n.Name, n.RxRate, n.TxRate, n.RxTotal, n.TxTotal)
	}

	for _, ss := range s.sessions {
		if ss.Session == "" {
			continue
		}
		batch.Queue(`
			INSERT INTO sessions_raw (ts, host_id, name, project, pct, tokens, messages, model, session_id, cwd)
			VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8, $9)
			ON CONFLICT DO NOTHING`,
			b.ts, hostID, ss.Session, ss.Pct, ss.Tokens, ss.Messages, ss.Model,
			nullable(ss.SessionID), nullable(ss.CWD))
	}

	if at := s.probes.At; at > 0 {
		ts := time.Unix(at, 0)
		for _, p := range s.probes.Items {
			if p.Name == "" || p.Kind == "" {
				continue
			}
			interval, timeout := 60, 5
			if p.Interval != nil {
				interval = min(max(*p.Interval, 10), 86400)
			}
			if p.Timeout != nil {
				timeout = min(max(*p.Timeout, 1), 60)
			}
			batch.Queue(`
				WITH p AS (
					INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, runner)
					VALUES ($2, $3, $4, $9, $10, 'agent')
					ON CONFLICT (name) DO UPDATE SET
						kind         = CASE WHEN probes.runner = 'agent' THEN excluded.kind         ELSE probes.kind         END,
						target       = CASE WHEN probes.runner = 'agent' THEN excluded.target       ELSE probes.target       END,
						interval_sec = CASE WHEN probes.runner = 'agent' THEN excluded.interval_sec ELSE probes.interval_sec END,
						timeout_sec  = CASE WHEN probes.runner = 'agent' THEN excluded.timeout_sec  ELSE probes.timeout_sec  END
					RETURNING id, enabled
				)
				INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
				SELECT $1, p.id, $5, $6, $7, $8 FROM p WHERE p.enabled
				ON CONFLICT DO NOTHING`,
				ts, p.Name, p.Kind, p.Target, p.OK, p.LatencyMS, p.Outcome, p.Error, interval, timeout)
		}
	}

	return tx.SendBatch(ctx, batch).Close()
}
