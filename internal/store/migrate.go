package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/deploy/migrations"
)

const advisoryLockKey int64 = 0x6d6f6e69

const migrateTimeout = 5 * time.Minute

const schemaVersionDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    integer     PRIMARY KEY,
    name       text        NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    checksum   text
);
ALTER TABLE schema_version ADD COLUMN IF NOT EXISTS checksum text;`

var fileName = regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
	sum     string
}

// ErrSchemaMismatch means a migration file changed after it was applied.
var ErrSchemaMismatch = errors.New("the schema does not match the migrations")

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	list, err := loadMigrations(migrations.FS)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("taking the migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
			log.Printf("store: the migration lock was not released: %v", err)
		}
	}()

	if _, err := conn.Exec(ctx, schemaVersionDDL); err != nil {
		return fmt.Errorf("the schema_version table: %w", err)
	}

	applied, err := appliedSums(ctx, conn)
	if err != nil {
		return fmt.Errorf("reading schema_version: %w", err)
	}

	var backfill []migration

	for _, m := range list {
		sum, done := applied[m.version]
		switch {
		case !done:
			if err := apply(ctx, conn, m); err != nil {
				return fmt.Errorf("%03d_%s: %w", m.version, m.name, err)
			}
			log.Printf("store: migration %03d_%s applied", m.version, m.name)
		case sum == nil:
			backfill = append(backfill, m)
		case *sum != m.sum:
			return fmt.Errorf(
				"%w: the file %03d_%s.sql changed after it was applied (%s… in the database, %s… in the file). "+
					"Either restore the previous content or roll the difference out as a separate migration: "+
					"the changed file cannot be applied silently, the schema is already a different one",
				ErrSchemaMismatch, m.version, m.name, (*sum)[:8], m.sum[:8])
		}
	}

	if len(backfill) > 0 {
		if err := fillSums(ctx, conn, backfill); err != nil {
			return fmt.Errorf("filling in the checksums: %w", err)
		}
		log.Printf("store: checksums filled in for %d migrations applied earlier", len(backfill))
	}

	for v := range applied {
		if !slices.ContainsFunc(list, func(m migration) bool { return m.version == v }) {
			log.Printf("store: the database holds migration %03d that the binary does not have — the database is newer than the code", v)
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgxpool.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = 0"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_version (version, name, checksum) VALUES ($1, $2, $3)",
		m.version, m.name, m.sum); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func appliedSums(ctx context.Context, conn *pgxpool.Conn) (map[int]*string, error) {
	var hasColumn bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'schema_version' AND column_name = 'checksum')`).Scan(&hasColumn); err != nil {
		return nil, err
	}

	query := "SELECT version, NULL::text FROM schema_version"
	if hasColumn {
		query = "SELECT version, checksum FROM schema_version"
	}
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]*string{}
	for rows.Next() {
		var version int
		var sum *string
		if err := rows.Scan(&version, &sum); err != nil {
			return nil, err
		}
		out[version] = sum
	}
	return out, rows.Err()
}

func fillSums(ctx context.Context, conn *pgxpool.Conn, list []migration) error {
	batch := &pgx.Batch{}
	for _, m := range list {
		batch.Queue("UPDATE schema_version SET checksum = $2 WHERE version = $1 AND checksum IS NULL",
			m.version, m.sum)
	}
	return conn.SendBatch(ctx, batch).Close()
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}

	out := make([]migration, 0, len(names))
	for _, name := range names {
		parts := fileName.FindStringSubmatch(name)
		if parts == nil {
			return nil, fmt.Errorf("migration %q: the name is not of the form NNN_name.sql", name)
		}
		version, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("migration %q: %w", name, err)
		}
		if i := slices.IndexFunc(out, func(m migration) bool { return m.version == version }); i >= 0 {
			return nil, fmt.Errorf("number %03d is taken twice: %s and %s", version, out[i].name, parts[2])
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			version: version, name: parts[2],
			sql: string(body), sum: hex.EncodeToString(sum[:]),
		})
	}

	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	return out, nil
}
