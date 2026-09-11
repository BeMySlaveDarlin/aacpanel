package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/store"
)

// ErrNoDevice means there is no device with such a key or number.
var ErrNoDevice = errors.New("no such device")

// Device is a registered passkey.
type Device struct {
	ID              int64
	Name            string
	CredentialID    []byte
	PublicKey       []byte
	SignCount       uint32
	AAGUID          []byte
	Transports      []string
	AttestationType string
	AttestationFmt  string
	BackupEligible  bool
	BackupState     bool
	CreatedAt       time.Time
	LastSeen        *time.Time
	RevokedAt       *time.Time
}

// DeviceStore is the device storage.
type DeviceStore interface {
	Add(ctx context.Context, d *Device) (int64, error)
	ByCredential(ctx context.Context, credentialID []byte) (*Device, error)
	List(ctx context.Context) ([]Device, error)
	Exists(ctx context.Context, id int64) (bool, error)
	Seen(ctx context.Context, id int64, signCount uint32, backupState bool, at time.Time) error
	Rename(ctx context.Context, id int64, name string) error
	Revoke(ctx context.Context, id int64) error
	Revoked(ctx context.Context) ([]Device, error)
}

// PoolSource returns a connection pool.
type PoolSource interface {
	Pool() (*pgxpool.Pool, error)
}

// Storage is everything passkey login keeps in the database.
type Storage interface {
	DeviceStore
	EnrollStore
	OwnerStore
	PushStore
}

type pgDevices struct {
	src PoolSource
}

// NewStore wraps a pool source into a storage.
func NewStore(src PoolSource) Storage {
	return &pgDevices{src: src}
}

const deviceColumns = `id, name, credential_id, public_key, sign_count, aaguid, transports,
	attestation_type, attestation_fmt, backup_eligible, backup_state, created_at, last_seen, revoked_at`

const live = ` AND revoked_at IS NULL`

func scanDevice(row pgx.CollectableRow) (Device, error) {
	var (
		d         Device
		signCount int64
	)
	err := row.Scan(&d.ID, &d.Name, &d.CredentialID, &d.PublicKey, &signCount, &d.AAGUID,
		&d.Transports, &d.AttestationType, &d.AttestationFmt, &d.BackupEligible,
		&d.BackupState, &d.CreatedAt, &d.LastSeen, &d.RevokedAt)
	if signCount < 0 || signCount > 0xFFFFFFFF {
		return d, fmt.Errorf("device %d: sign_count is out of range: %d", d.ID, signCount)
	}
	d.SignCount = uint32(signCount)
	return d, err
}

func (p *pgDevices) Add(ctx context.Context, d *Device) (int64, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return 0, err
	}
	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key, sign_count, aaguid, transports,
		                     attestation_type, attestation_fmt, backup_eligible, backup_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		d.Name, d.CredentialID, d.PublicKey, int64(d.SignCount), d.AAGUID, d.Transports,
		d.AttestationType, d.AttestationFmt, d.BackupEligible, d.BackupState).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("saving the device: %w", store.Unavailable(err))
	}
	return id, nil
}

func (p *pgDevices) ByCredential(ctx context.Context, credentialID []byte) (*Device, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+deviceColumns+` FROM devices WHERE credential_id = $1`+live, credentialID)
	if err != nil {
		return nil, fmt.Errorf("looking up the device: %w", store.Unavailable(err))
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDevice)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoDevice
	}
	if err != nil {
		return nil, fmt.Errorf("looking up the device: %w", store.Unavailable(err))
	}
	return &d, nil
}

func (p *pgDevices) List(ctx context.Context) ([]Device, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE true`+live+` ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("device list: %w", store.Unavailable(err))
	}
	list, err := pgx.CollectRows(rows, scanDevice)
	if err != nil {
		return nil, fmt.Errorf("device list: %w", store.Unavailable(err))
	}
	return list, nil
}

func (p *pgDevices) Exists(ctx context.Context, id int64) (bool, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return false, err
	}
	var ok bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1`+live+`)`, id).Scan(&ok); err != nil {
		return false, fmt.Errorf("checking the device: %w", store.Unavailable(err))
	}
	return ok, nil
}

func (p *pgDevices) Seen(ctx context.Context, id int64, signCount uint32, backupState bool, at time.Time) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `
		UPDATE devices SET sign_count = $2, backup_state = $3, last_seen = $4
		 WHERE id = $1`+live,
		id, int64(signCount), backupState, at)
	if err != nil {
		return fmt.Errorf("marking the login: %w", store.Unavailable(err))
	}
	if tag.RowsAffected() == 0 {
		return ErrNoDevice
	}
	return nil
}

func (p *pgDevices) Rename(ctx context.Context, id int64, name string) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `UPDATE devices SET name = $2 WHERE id = $1`+live, id, name)
	if err != nil {
		return fmt.Errorf("renaming the device: %w", store.Unavailable(err))
	}
	if tag.RowsAffected() == 0 {
		return ErrNoDevice
	}
	return nil
}

func (p *pgDevices) Revoke(ctx context.Context, id int64) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("revoking the device: %w", store.Unavailable(err))
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM push_subscriptions WHERE device_id = $1`, id); err != nil {
		return fmt.Errorf("revoking the device: %w", store.Unavailable(err))
	}
	tag, err := tx.Exec(ctx, `
		UPDATE devices SET revoked_at = now(), credential_id = NULL, public_key = NULL
		 WHERE id = $1`+live, id)
	if err != nil {
		return fmt.Errorf("revoking the device: %w", store.Unavailable(err))
	}
	if tag.RowsAffected() == 0 {
		return ErrNoDevice
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("revoking the device: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) Revoked(ctx context.Context) ([]Device, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE revoked_at IS NOT NULL ORDER BY revoked_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("revoked devices: %w", store.Unavailable(err))
	}
	list, err := pgx.CollectRows(rows, scanDevice)
	if err != nil {
		return nil, fmt.Errorf("revoked devices: %w", store.Unavailable(err))
	}
	return list, nil
}

func (p *pgDevices) AddCode(ctx context.Context, hash, source string, expires time.Time) error {
	pool, err := p.src.Pool()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM enroll_codes WHERE expires_at < now() - interval '1 day'`); err != nil {
		return fmt.Errorf("clearing out spent codes: %w", store.Unavailable(err))
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO enroll_codes (code_hash, source, expires_at) VALUES ($1, $2, $3)`,
		hash, source, expires); err != nil {
		return fmt.Errorf("saving the code: %w", store.Unavailable(err))
	}
	return nil
}

func (p *pgDevices) ActiveCodes(ctx context.Context) ([]EnrollCode, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, code_hash FROM enroll_codes WHERE used_at IS NULL AND expires_at > now() ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("active codes: %w", store.Unavailable(err))
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EnrollCode, error) {
		var c EnrollCode
		return c, row.Scan(&c.ID, &c.Hash)
	})
	if err != nil {
		return nil, fmt.Errorf("active codes: %w", store.Unavailable(err))
	}
	return list, nil
}

func (p *pgDevices) UseCode(ctx context.Context, id int64) (bool, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return false, err
	}
	tag, err := pool.Exec(ctx,
		`UPDATE enroll_codes SET used_at = now() WHERE id = $1 AND used_at IS NULL AND expires_at > now()`, id)
	if err != nil {
		return false, fmt.Errorf("burning the code: %w", store.Unavailable(err))
	}
	return tag.RowsAffected() == 1, nil
}

func (p *pgDevices) UserHandle(ctx context.Context) ([]byte, error) {
	pool, err := p.src.Pool()
	if err != nil {
		return nil, err
	}
	var handle []byte
	if err := pool.QueryRow(ctx, `SELECT user_handle FROM owner WHERE id = 1`).Scan(&handle); err != nil {
		return nil, fmt.Errorf("the user handle of the owner: %w", store.Unavailable(err))
	}
	return handle, nil
}
