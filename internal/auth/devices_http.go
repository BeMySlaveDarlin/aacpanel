package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"
)

type deviceJSON struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen"`
	SignCount uint32     `json:"sign_count"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func deviceView(d Device) deviceJSON {
	return deviceJSON{
		ID:        d.ID,
		Name:      d.Name,
		CreatedAt: d.CreatedAt,
		LastSeen:  d.LastSeen,
		SignCount: d.SignCount,
		RevokedAt: d.RevokedAt,
	}
}

// ListDevices returns the list of registered devices.
func (p *Passkey) ListDevices(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	list, err := p.store.List(ctx)
	if err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}
	gone, err := p.store.Revoked(ctx)
	if err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}

	out := make([]deviceJSON, 0, len(list))
	for _, d := range list {
		out = append(out, deviceView(d))
	}
	revoked := make([]deviceJSON, 0, len(gone))
	for _, d := range gone {
		revoked = append(revoked, deviceView(d))
	}
	current := p.session.CurrentDevice(r)
	if current < 0 {
		current = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": out,
		"revoked": revoked,
		"current": current,
	})
}

// RenameDevice changes the name of a device.
func (p *Passkey) RenameDevice(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "the request body is malformed")
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	name := deviceName(body.Name)
	switch err := p.store.Rename(ctx, id, name); {
	case errors.Is(err, ErrNoDevice):
		fail(w, http.StatusNotFound, "there is no such device")
	case err != nil:
		dbFail(w, err, "the database could not be reached")
	default:
		log.Printf("%s: device %d renamed to %q", ClientIP(r), id, name)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
	}
}

// RevokeDevice revokes a single device.
func (p *Passkey) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	switch err := p.store.Revoke(ctx, id); {
	case errors.Is(err, ErrNoDevice):
		fail(w, http.StatusNotFound, "there is no such device")
	case err != nil:
		dbFail(w, err, "the database could not be reached")
	default:
		p.session.ForgetDevice(id)
		log.Printf("%s: device %d revoked", ClientIP(r), id)
		if p.session.CurrentDevice(r) == id {
			p.session.Clear(w, p.secure)
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": id})
	}
}

func deviceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, "a device id is required")
		return 0, false
	}
	return id, true
}

// DeviceName returns the name of the device with this number.
func (p *Passkey) DeviceName(ctx context.Context, id int64) string {
	if id < 0 {
		return "token login"
	}
	if !p.Enabled() || id == 0 {
		return ""
	}
	list, err := p.store.List(ctx)
	if err != nil {
		return ""
	}
	for _, d := range list {
		if d.ID == id {
			return d.Name
		}
	}
	return ""
}
