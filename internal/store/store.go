package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUnavailable is returned while the pool is not up.
var ErrUnavailable = errors.New("the database is unavailable")

// ErrClosed means the store is shutting down.
var ErrClosed = errors.New("the store is closed")

const (
	connectTimeout   = 10 * time.Second
	retryMin         = 2 * time.Second
	retryMax         = 30 * time.Second
	partitionEvery   = 6 * time.Hour
	statementTimeout = "30s"
)

// Store is access to Postgres.
type Store struct {
	cfg      *pgxpool.Config
	ownerCfg *pgxpool.Config

	mu     sync.RWMutex
	pool   *pgxpool.Pool
	owner  *pgxpool.Pool
	closed bool

	hostMu  sync.Mutex
	hostIDs map[string]int

	projectRoots []string
}

// New parses the DSN and prepares the pool config.
func New(dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing AACP_DB_DSN: %w", err)
	}

	cfg.MaxConns = 8
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	cfg.ConnConfig.ConnectTimeout = connectTimeout
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "aacpanel"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeout
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60s"

	return &Store{cfg: cfg, hostIDs: map[string]int{}}, nil
}

// UseOwner sets a separate connection under the schema owner.
func (s *Store) UseOwner(dsn string) error {
	if dsn == "" {
		return nil
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parsing AACP_DB_MIGRATE_DSN: %w", err)
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = time.Minute
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "aacpanel-owner"
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	s.ownerCfg = cfg
	return nil
}

func (s *Store) ownerPool() (*pgxpool.Pool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.owner != nil {
		return s.owner, nil
	}
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	return s.pool, nil
}

// Open connects once and applies the migrations, with no background loops.
func (s *Store) Open(ctx context.Context) error {
	return s.connect(ctx, true, false)
}

// Run connects to the database, applies the migrations and starts the housekeeping.
func (s *Store) Run(ctx context.Context) error {
	delay := retryMin
	for {
		err := s.connect(ctx, true, true)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrSchemaMismatch) {
			return err
		}
		log.Printf("store: %v; retrying in %s", err, delay)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		if delay *= 2; delay > retryMax {
			delay = retryMax
		}
	}
}

func (s *Store) connect(ctx context.Context, migrations, background bool) error {
	pool, err := pgxpool.NewWithConfig(ctx, s.cfg)
	if err != nil {
		return fmt.Errorf("connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		pool.Close()
		return fmt.Errorf("connecting: %w", err)
	}

	var owner *pgxpool.Pool
	if s.ownerCfg != nil {
		owner, err = pgxpool.NewWithConfig(ctx, s.ownerCfg)
		if err != nil {
			pool.Close()
			return fmt.Errorf("the owner connection pool: %w", err)
		}
		pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		err = owner.Ping(pingCtx)
		cancel()
		if err != nil {
			owner.Close()
			pool.Close()
			return fmt.Errorf("connecting as the owner: %w", err)
		}
	}

	if migrations {
		migrator := pool
		if owner != nil {
			migrator = owner
		}
		if err := migrate(ctx, migrator); err != nil {
			if owner != nil {
				owner.Close()
			}
			pool.Close()
			return fmt.Errorf("migrations: %w", err)
		}
		if err := s.grantApp(ctx, owner); err != nil {
			if owner != nil {
				owner.Close()
			}
			pool.Close()
			return fmt.Errorf("grants for the application role: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if owner != nil {
			owner.Close()
		}
		pool.Close()
		return nil
	}
	s.pool = pool
	s.owner = owner
	log.Printf("store: connected to %s/%s", s.cfg.ConnConfig.Host, s.cfg.ConnConfig.Database)

	if n, err := closeStaleAttachments(ctx, pool); err != nil {
		log.Printf("store: stale terminal attachments are not closed: %v", err)
	} else if n > 0 {
		log.Printf("store: terminal attachments left by the previous run closed: %d", n)
	}

	if background {
		go s.keepPartitions(ctx)
		go s.keepRollups(ctx)
		go s.keepRetention(ctx)
	}
	return nil
}

func (s *Store) keepPartitions(ctx context.Context) {
	t := time.NewTicker(partitionEvery)
	defer t.Stop()

	for {
		err := s.ensurePartitions(ctx)
		switch {
		case err == nil, ctx.Err() != nil:
		case errors.Is(err, ErrClosed):
			return
		default:
			log.Printf("store: cutting partitions: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Store) ensurePartitions(ctx context.Context) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	var made int
	if err := pool.QueryRow(ctx, "SELECT ensure_partitions()").Scan(&made); err != nil {
		return err
	}
	if made > 0 {
		log.Printf("store: partitions created: %d", made)
	}
	return nil
}

// Pool returns the pool, or ErrUnavailable if the database is not available.
func (s *Store) Pool() (*pgxpool.Pool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	return s.pool, nil
}

// HostID returns the host id by name, creating the row on first use.
func (s *Store) HostID(ctx context.Context, name string) (int, error) {
	s.hostMu.Lock()
	id, ok := s.hostIDs[name]
	s.hostMu.Unlock()
	if ok {
		return id, nil
	}

	pool, err := s.Pool()
	if err != nil {
		return 0, err
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO hosts (name, kind) VALUES ($1, 'host')
		ON CONFLICT (name) DO UPDATE SET name = excluded.name
		RETURNING id`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("host %q: %w", name, Unavailable(err))
	}

	s.hostMu.Lock()
	s.hostIDs[name] = id
	s.hostMu.Unlock()
	return id, nil
}

// Ready reports whether the pool is up.
func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool != nil
}

// Close closes the pool.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.pool != nil {
		s.pool.Close()
		s.pool = nil
	}
	if s.owner != nil {
		s.owner.Close()
		s.owner = nil
	}
}
