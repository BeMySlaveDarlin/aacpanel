package store

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/secure/precis"
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
	return applyAppRole(ctx, owner, role, s.cfg.ConnConfig.Password)
}

func grantAppRole(ctx context.Context, owner *pgxpool.Pool, role string) error {
	return applyAppRole(ctx, owner, role, "")
}

func applyAppRole(ctx context.Context, owner *pgxpool.Pool, role, password string) error {
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

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if password != "" {
		// Postgres accepts a SCRAM verifier as the password. The plaintext
		// never reaches SQL, even on a server that logs bind parameters or
		// the expanded statement of a failed EXECUTE. Transaction-local
		// settings keep the verifier out of the next borrower's connection.
		verifier, err := appPasswordVerifier(password)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('aacpanel.app_role', $1, true),
			set_config('aacpanel.app_password', $2, true)`, role, verifier); err != nil {
			return fmt.Errorf("preparing the application role: %s", strings.NewReplacer(password, "[redacted]", verifier, "[redacted]").Replace(err.Error()))
		}
		if _, err := tx.Exec(ctx, `DO $$
		DECLARE
			app_role text := current_setting('aacpanel.app_role');
			app_password text := current_setting('aacpanel.app_password');
		BEGIN
			IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = app_role) THEN
				EXECUTE format('ALTER ROLE %I LOGIN PASSWORD %L', app_role, app_password);
			ELSE
				EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', app_role, app_password);
			END IF;
		END $$`); err != nil {
			return fmt.Errorf("setting the application role password: %s", strings.NewReplacer(password, "[redacted]", verifier, "[redacted]").Replace(err.Error()))
		}
	}

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

func appPasswordVerifier(password string) (string, error) {
	const iterations = 4096
	salt := make([]byte, 16)
	rand.Read(salt)
	// Match PostgreSQL's SCRAM password preparation, including its fallback
	// for passwords SASLprep cannot represent.
	prepared, err := precis.OpaqueString.String(password)
	if err != nil {
		prepared = password
	}
	key, err := pbkdf2.Key(sha256.New, prepared, salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("preparing the application password verifier: %w", err)
	}
	mac := func(label string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(label))
		return h.Sum(nil)
	}
	stored := sha256.Sum256(mac("Client Key"))
	b64 := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, b64(salt), b64(stored[:]), b64(mac("Server Key"))), nil
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
