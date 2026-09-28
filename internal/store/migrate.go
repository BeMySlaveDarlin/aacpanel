package store

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/deploy/migrations"
)

const advisoryLockKey int64 = 0x6d6f6e69

const migrateTimeout = 5 * time.Minute

// schemaVersionDDL shapes the log of applied migrations. A file whose number is
// in the log is never run again and never compared with anything, so the log
// keeps no trace of a file's text. The shape is set here, at every start, and
// not by a migration: a migration runs once, and a database that has run every
// file already has to come to the same table as a fresh one.
const schemaVersionDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    integer     PRIMARY KEY,
    name       text        NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE schema_version DROP COLUMN IF EXISTS checksum;`

var fileName = regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version int
	name    string
	sql     string
}

type logged struct {
	version int
	name    string
}

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

	applied, err := appliedLog(ctx, conn)
	if err != nil {
		return fmt.Errorf("reading schema_version: %w", err)
	}

	for _, m := range list {
		if i := slices.IndexFunc(applied, func(l logged) bool { return l.version == m.version }); i >= 0 {
			if l := applied[i]; l.name != m.name {
				log.Printf("store: migration %03d is logged as %s but the file is %03d_%s: "+
					"the number was reused, the file will not run", m.version, l.name, m.version, m.name)
			}
			continue
		}
		if err := apply(ctx, conn, m); err != nil {
			return fmt.Errorf("%03d_%s: %w", m.version, m.name, err)
		}
		log.Printf("store: migration %03d_%s applied", m.version, m.name)
	}

	for _, l := range applied {
		if !slices.ContainsFunc(list, func(m migration) bool { return m.version == l.version }) {
			log.Printf("store: migration %03d_%s is logged as applied but has no file: "+
				"it was removed from the tree, or the database is newer than the code; left as it is",
				l.version, l.name)
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
		"INSERT INTO schema_version (version, name) VALUES ($1, $2)",
		m.version, m.name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func appliedLog(ctx context.Context, conn *pgxpool.Conn) ([]logged, error) {
	rows, err := conn.Query(ctx, "SELECT version, name FROM schema_version ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []logged
	for rows.Next() {
		var l logged
		if err := rows.Scan(&l.version, &l.name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
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
		out = append(out, migration{version: version, name: parts[2], sql: string(body)})
	}

	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	return out, nil
}
