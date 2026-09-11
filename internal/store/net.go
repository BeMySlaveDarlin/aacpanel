package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NetHistory is the traffic history per interface.
type NetHistory struct {
	Resolution string `json:"resolution"`
	StepSec    int    `json:"stepSec"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`

	T      []int64     `json:"t"`
	Ifaces []NetSeries `json:"ifaces"`
}

// NetSeries is one interface: the average and the peak rate per bucket.
type NetSeries struct {
	Name    string     `json:"name"`
	Rx      []*float64 `json:"rx"`
	Tx      []*float64 `json:"tx"`
	RxMax   []*float64 `json:"rxMax"`
	TxMax   []*float64 `json:"txMax"`
	RxTotal float64    `json:"rxTotal"`
	TxTotal float64    `json:"txTotal"`
}

// NetReq says for which period and with which downsampling.
type NetReq struct {
	HostID     int
	From, To   time.Time
	MaxPoints  int
	Resolution string
}

// NetFor returns the per-interface traffic for a period.
func (s *Store) NetFor(ctx context.Context, req NetReq) (*NetHistory, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	from, to := req.From, req.To

	res := req.Resolution
	if res == "" {
		res = s.pickResolution(ctx, "metrics_net", from, to)
	}
	table, tsCol, err := tableFor("metrics_net", res)
	if err != nil {
		return nil, err
	}

	step := stepFor(res, from, to, req.MaxPoints)
	out := &NetHistory{
		Resolution: res, StepSec: int(step.Seconds()),
		From: from.Unix(), To: to.Unix(),
		T: []int64{}, Ifaces: []NetSeries{},
	}

	names, err := s.netIfaces(ctx, pool, table, tsCol, req)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return out, nil
	}

	rxAvg, rxMax, txAvg, txMax := "rx_avg", "rx_max", "tx_avg", "tx_max"
	if res == ResRaw {
		rxAvg, rxMax, txAvg, txMax = "rx_rate", "rx_rate", "tx_rate", "tx_rate"
	}

	q := fmt.Sprintf(`
		WITH d AS (
			SELECT iface,
			       date_bin($1, %[2]s, timestamptz '%[1]s') AS b,
			       avg(%[4]s)::float8 AS rx,  max(%[5]s)::float8 AS rx_max,
			       avg(%[6]s)::float8 AS tx,  max(%[7]s)::float8 AS tx_max
			FROM %[3]s
			WHERE host_id = $2 AND %[2]s >= $3 AND %[2]s < $4
			GROUP BY 1, 2
		), span AS (
			SELECT min(b) AS lo, max(b) AS hi FROM d
		), grid AS (
			SELECT g FROM span CROSS JOIN LATERAL generate_series(span.lo, span.hi, $1) AS g
		)
		SELECT extract(epoch FROM grid.g)::bigint, n.iface, d.rx, d.rx_max, d.tx, d.tx_max
		FROM grid
		CROSS JOIN unnest($5::text[]) AS n(iface)
		LEFT JOIN d ON d.b = grid.g AND d.iface = n.iface
		ORDER BY 1, 2`, binOrigin, tsCol, table, rxAvg, rxMax, txAvg, txMax)

	rows, err := pool.Query(ctx, q, step, req.HostID, from, to, names)
	if err != nil {
		return nil, Unavailable(err)
	}
	defer rows.Close()

	byName := map[string]*NetSeries{}
	for _, name := range names {
		byName[name] = &NetSeries{Name: name,
			Rx: []*float64{}, Tx: []*float64{}, RxMax: []*float64{}, TxMax: []*float64{}}
	}
	var lastT int64
	for rows.Next() {
		var t int64
		var iface string
		var rx, rxMax, tx, txMax *float64
		if err := rows.Scan(&t, &iface, &rx, &rxMax, &tx, &txMax); err != nil {
			return nil, Unavailable(err)
		}
		if len(out.T) == 0 || t != lastT {
			out.T = append(out.T, t)
			lastT = t
		}
		row := byName[iface]
		if row == nil {
			continue
		}
		row.Rx = append(row.Rx, rx)
		row.RxMax = append(row.RxMax, rxMax)
		row.Tx = append(row.Tx, tx)
		row.TxMax = append(row.TxMax, txMax)
	}
	if err := rows.Err(); err != nil {
		return nil, Unavailable(err)
	}

	if err := s.netTotals(ctx, pool, table, tsCol, res, req, byName); err != nil {
		return nil, err
	}

	for _, name := range names {
		out.Ifaces = append(out.Ifaces, *byName[name])
	}
	return out, nil
}

func (s *Store) netTotals(ctx context.Context, pool *pgxpool.Pool,
	table, tsCol, res string, req NetReq, byName map[string]*NetSeries) error {

	q := fmt.Sprintf(`
		SELECT iface, sum(rx_bytes)::float8, sum(tx_bytes)::float8
		FROM %s WHERE host_id = $1 AND %s >= $2 AND %s < $3
		GROUP BY iface`, table, tsCol, tsCol)

	if res == ResRaw {
		q = fmt.Sprintf(`
			WITH d AS (
				SELECT iface,
				       rx_total - lag(rx_total) OVER w AS rx_delta,
				       tx_total - lag(tx_total) OVER w AS tx_delta
				FROM %s
				WHERE host_id = $1 AND %s >= $2 AND %s < $3
				WINDOW w AS (PARTITION BY iface ORDER BY %s)
			)
			SELECT iface, sum(greatest(rx_delta, 0))::float8, sum(greatest(tx_delta, 0))::float8
			FROM d GROUP BY iface`, table, tsCol, tsCol, tsCol)
	}

	rows, err := pool.Query(ctx, q, req.HostID, req.From, req.To)
	if err != nil {
		return Unavailable(err)
	}
	defer rows.Close()

	for rows.Next() {
		var iface string
		var rx, tx *float64
		if err := rows.Scan(&iface, &rx, &tx); err != nil {
			return Unavailable(err)
		}
		row := byName[iface]
		if row == nil {
			continue
		}
		if rx != nil {
			row.RxTotal = *rx
		}
		if tx != nil {
			row.TxTotal = *tx
		}
	}
	return Unavailable(rows.Err())
}

func (s *Store) netIfaces(ctx context.Context, pool *pgxpool.Pool, table, tsCol string, req NetReq) ([]string, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT DISTINCT iface FROM %s
		 WHERE host_id = $1 AND %s >= $2 AND %s < $3 ORDER BY iface`, table, tsCol, tsCol),
		req.HostID, req.From, req.To)
	if err != nil {
		return nil, Unavailable(err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, Unavailable(err)
		}
		out = append(out, name)
	}
	return out, Unavailable(rows.Err())
}
