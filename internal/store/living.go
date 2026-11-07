package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// LivingSession keeps the last census separately from the metric archive:
// rollups cannot distinguish a conversation closed before a host stopped.
type LivingSession struct {
	Conversation                                       string
	Session                                            string
	ProjectID                                          int
	Project, Contour, ConfigDir, CWD, Transport, Agent string
	Parent, ParentConversation                         string
	OpenedBy                                           string
	Outside                                            bool
	Pause                                              json.RawMessage
	BootID                                             string
	SeenAt                                             time.Time
}

// KeepLiving replaces one boot's census atomically. Absent conversations stay
// available for a manual resume, but cannot be revived after a reboot.
func (s *Store) KeepLiving(ctx context.Context, host int, boot string, at time.Time, live []LivingSession) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// All census writers for this host agree on the order of its boots.
	if _, err = tx.Exec(ctx, `SELECT id FROM hosts WHERE id=$1 FOR UPDATE`, host); err != nil {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `INSERT INTO session_boots (host_id,boot_id,snapshot_at) VALUES ($1,$2,$3)
		ON CONFLICT (host_id,boot_id) DO UPDATE SET snapshot_at=excluded.snapshot_at
		WHERE session_boots.snapshot_at < excluded.snapshot_at RETURNING true`, host, boot, at).Scan(&accepted)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE session_living SET live=false WHERE host_id=$1 AND boot_id=$2`, host, boot); err != nil {
		return err
	}
	for _, row := range live {
		if row.Conversation == "" {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO session_living
			(host_id,boot_id,conversation,session,project_id,project,contour,config_dir,cwd,transport,agent,
			parent,parent_conversation,outside,pause,seen_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (host_id,boot_id,conversation) DO UPDATE SET session=$4,project_id=$5,project=$6,contour=$7,
			config_dir=$8,cwd=$9,transport=$10,agent=$11,parent=$12,parent_conversation=$13,outside=$14,pause=$15,seen_at=$16,live=true`,
			host, boot, row.Conversation, row.Session, row.ProjectID, row.Project, row.Contour, row.ConfigDir, row.CWD,
			row.Transport, row.Agent, row.Parent, row.ParentConversation, row.Outside, row.Pause, at)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const livingColumns = `conversation,session,project_id,project,contour,config_dir,cwd,transport,agent,
	parent,parent_conversation,outside,pause,boot_id,seen_at`

func scanLiving(row pgx.CollectableRow) (LivingSession, error) {
	var s LivingSession
	err := row.Scan(&s.Conversation, &s.Session, &s.ProjectID, &s.Project, &s.Contour, &s.ConfigDir, &s.CWD, &s.Transport,
		&s.Agent, &s.Parent, &s.ParentConversation, &s.Outside, &s.Pause, &s.BootID, &s.SeenAt)
	return s, err
}

// ClaimRevival marks a boot before any host action. A deployment or a crash
// during recovery cannot start a second set of processes on the transcripts.
func (s *Store) ClaimRevival(ctx context.Context, host int, boot string) (previous string, live []LivingSession, claimed bool, err error) {
	pool, err := s.Pool()
	if err != nil {
		return "", nil, false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", nil, false, err
	}
	defer tx.Rollback(context.Background())
	var id int64
	err = tx.QueryRow(ctx, `UPDATE session_boots SET claimed=true WHERE host_id=$1 AND boot_id=$2 AND NOT claimed RETURNING id`, host, boot).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	err = tx.QueryRow(ctx, `SELECT boot_id FROM session_boots WHERE host_id=$1 AND id<$2 ORDER BY id DESC LIMIT 1`, host, id).Scan(&previous)
	if err != nil && err != pgx.ErrNoRows {
		return "", nil, false, err
	}
	rows, err := tx.Query(ctx, `SELECT `+livingColumns+` FROM session_living WHERE host_id=$1 AND boot_id=$2 AND live ORDER BY session,conversation`, host, previous)
	if err != nil {
		return "", nil, false, err
	}
	live, err = pgx.CollectRows(rows, scanLiving)
	if err != nil {
		return "", nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", nil, false, err
	}
	return previous, live, true, nil
}

// LivingConversation finds the parent and place of an archived conversation,
// even when it was too short to reach a metric rollup.
func (s *Store) LivingConversation(ctx context.Context, host int, conversation string) (*LivingSession, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+livingColumns+` FROM session_living WHERE host_id=$1 AND conversation=$2 ORDER BY seen_at DESC LIMIT 1`, host, conversation)
	if err != nil {
		return nil, err
	}
	row, err := pgx.CollectOneRow(rows, scanLiving)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &row, err
}
