package store

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var appPrivs = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}

var appOutcomeColumns = []string{"result", "error", "detail", "duration_ms", "finished_at"}

type tableGrant struct {
	table         string
	privs         []string
	updateColumns []string
}

var appTableGrants = []tableGrant{
	{
		table:         "actions",
		privs:         []string{"SELECT", "INSERT"},
		updateColumns: appOutcomeColumns,
	},
	{
		table: "partition_config",
		privs: []string{"SELECT"},
	},
	{
		table: "disk_hidden",
		privs: []string{"SELECT", "INSERT", "DELETE"},
	},
}

func (s *Store) grantApp(ctx context.Context, owner *pgxpool.Pool) error {
	if owner == nil {
		return nil
	}
	role := s.cfg.ConnConfig.User
	if role == "" {
		return nil
	}
	return grantAppRole(ctx, owner, role)
}

func grantAppRole(ctx context.Context, owner *pgxpool.Pool, role string) error {
	conn, err := owner.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var db, current string
	if err := conn.QueryRow(ctx, "SELECT current_database(), current_user").Scan(&db, &current); err != nil {
		return fmt.Errorf("asking the database who we are: %w", err)
	}
	if role == current {
		return nil
	}

	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return fmt.Errorf("looking for role %q: %w", role, err)
	}
	if !exists {
		log.Printf("store: there is no role %q in the database — no grants are issued; create the role with deploy/create-app-role.sh", role)
		return nil
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	for _, stmt := range appGrantSQL(db, current, role) {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	log.Printf("store: grants for role %s issued", role)
	return nil
}

func appGrantSQL(db, owner, role string) []string {
	dbID, ownerID, roleID := quoteIdent(db), quoteIdent(owner), quoteIdent(role)
	all := strings.Join(appPrivs, ", ")

	out := []string{
		"GRANT CONNECT ON DATABASE " + dbID + " TO " + roleID,
		"GRANT USAGE ON SCHEMA public TO " + roleID,

		"GRANT " + all + " ON ALL TABLES IN SCHEMA public TO " + roleID,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + roleID,

		"ALTER DEFAULT PRIVILEGES FOR ROLE " + ownerID + " IN SCHEMA public GRANT " + all + " ON TABLES TO " + roleID,
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + ownerID + " IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + roleID,

		"GRANT EXECUTE ON FUNCTION ensure_partitions(interval) TO " + roleID,
	}

	for _, g := range appTableGrants {
		table := quoteIdent(g.table)
		out = append(out, "REVOKE ALL ON "+table+" FROM "+roleID)
		if len(g.privs) > 0 {
			out = append(out, "GRANT "+strings.Join(g.privs, ", ")+" ON "+table+" TO "+roleID)
		}
		if len(g.updateColumns) > 0 {
			out = append(out, "GRANT UPDATE ("+strings.Join(g.updateColumns, ", ")+") ON "+table+" TO "+roleID)
		}
	}
	return out
}
