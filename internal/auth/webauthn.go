package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"aacpanel/internal/store"
)

const deviceNameMax = 64

// PasskeyConfig holds the relying party settings.
type PasskeyConfig struct {
	RPID    string
	RPName  string
	Origins []string
	Secure  bool
}

// Passkey is WebAuthn login: device registration, sign-in and list management.
type Passkey struct {
	wa      *webauthn.WebAuthn
	store   Storage
	session *Service
	pending *pendingStore
	name    string
	secure  bool
	enabled bool

	hmu    sync.Mutex
	handle []byte
}

// NewPasskey builds passkey login, disabled when the store is nil.
func NewPasskey(cfg PasskeyConfig, session *Service, store Storage) (*Passkey, error) {
	if session == nil {
		return nil, errors.New("passkey: a session service is required")
	}
	if cfg.RPID == "" {
		return nil, errors.New("passkey: AACP_RP_ID is empty")
	}
	origins := make([]string, 0, len(cfg.Origins))
	for _, o := range cfg.Origins {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return nil, errors.New("passkey: AACP_RP_ORIGINS is empty")
	}

	wa, err := webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.RPName,
		RPOrigins:             origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("passkey: %w", err)
	}

	name := cfg.RPName
	if name == "" {
		name = cfg.RPID
	}

	return &Passkey{
		wa:      wa,
		store:   store,
		session: session,
		pending: newPendingStore(),
		name:    name,
		secure:  cfg.Secure,
		enabled: store != nil,
	}, nil
}

func (p *Passkey) userHandle(ctx context.Context) ([]byte, error) {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	if p.handle != nil {
		return p.handle, nil
	}
	handle, err := p.store.UserHandle(ctx)
	if err != nil {
		return nil, err
	}
	if len(handle) == 0 {
		return nil, errors.New("passkey: the owner has an empty user handle")
	}
	p.handle = handle
	return handle, nil
}

// Enabled reports whether passkey login can be used at all.
func (p *Passkey) Enabled() bool { return p != nil && p.enabled }

type waUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (d *Device) credential() webauthn.Credential {
	return webauthn.Credential{
		ID:                d.CredentialID,
		PublicKey:         d.PublicKey,
		AttestationType:   d.AttestationType,
		AttestationFormat: d.AttestationFmt,
		Transport:         transports(d.Transports),
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: d.BackupEligible,
			BackupState:    d.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    d.AAGUID,
			SignCount: d.SignCount,
		},
	}
}

func transports(list []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, 0, len(list))
	for _, t := range list {
		out = append(out, protocol.AuthenticatorTransport(t))
	}
	return out
}

func transportStrings(list []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, string(t))
	}
	return out
}

type registerBeginRequest struct {
	Code string `json:"code"`
}

// BeginRegister starts the registration of a new device.
func (p *Passkey) BeginRegister(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	ip := ClientIP(r)
	if wait := p.session.Throttle(ip); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, wait %d s", int(wait.Seconds())+1))
		return
	}

	var body registerBeginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "the request body is malformed")
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	codeID, err := MatchEnrollCode(ctx, p.store, body.Code)
	switch {
	case errors.Is(err, ErrNoCode):
		p.session.Fail(ip)
		log.Printf("%s: registration turned down — the code does not work", ip)
		fail(w, http.StatusForbidden, "the code does not work: it is wrong, expired or already used")
		return
	case err != nil:
		dbFail(w, err, "checking the code")
		return
	}

	handle, err := p.userHandle(ctx)
	if err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}

	known, err := p.store.List(ctx)
	if err != nil {
		dbFail(w, err, "the device list")
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(known))
	for _, d := range known {
		exclude = append(exclude, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: d.CredentialID,
			Transport:    transports(d.Transports),
		})
	}

	user := &waUser{id: handle, name: p.name}
	creation, session, err := p.wa.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		log.Printf("passkey: starting the registration: %v", err)
		fail(w, http.StatusInternalServerError, "the registration could not be started")
		return
	}

	key, err := p.pending.put(ceremonyRegister, *session, codeID)
	if err != nil {
		log.Printf("passkey: the registration state was not saved: %v", err)
		fail(w, http.StatusInternalServerError, "the registration could not be started")
		return
	}
	p.session.Ok(ip)
	setCeremonyCookie(w, key, p.secure)
	writeJSON(w, http.StatusOK, creation)
}

// IssueCode issues an enrollment code from an already registered device.
func (p *Passkey) IssueCode(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	if !p.session.Authorized(r) {
		fail(w, http.StatusUnauthorized, "not authorized")
		return
	}
	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	code, expires, err := NewEnrollCode(ctx, p.store, "panel")
	if err != nil {
		dbFail(w, err, "issuing the code")
		return
	}
	log.Printf("%s: an enrolment code was issued from device %d, good for %s",
		ClientIP(r), p.session.CurrentDevice(r), EnrollTTL)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expires_at": expires})
}

type registerRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// FinishRegister checks the authenticator response and registers the device.
func (p *Passkey) FinishRegister(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}

	key, err := r.Cookie(ceremonyCookie)
	if err != nil {
		fail(w, http.StatusBadRequest, "the registration was not started")
		return
	}
	clearCeremonyCookie(w, p.secure)
	started, ok := p.pending.take(key.Value, ceremonyRegister)
	if !ok {
		fail(w, http.StatusBadRequest, "the registration was not started, or it has expired: start over")
		return
	}

	var body registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "the request body is malformed")
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
	if err != nil {
		log.Printf("passkey: parsing the registration response: %v", detail(err))
		fail(w, http.StatusBadRequest, "the authenticator answered with something unreadable")
		return
	}

	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	handle, err := p.userHandle(ctx)
	if err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}

	user := &waUser{id: handle, name: p.name}
	cred, err := p.wa.CreateCredential(user, started.data, parsed)
	if err != nil {
		log.Printf("passkey: the registration was rejected: %v", detail(err))
		fail(w, http.StatusBadRequest, "the registration did not pass the check")
		return
	}

	used, err := p.store.UseCode(ctx, started.code)
	if err != nil {
		dbFail(w, err, "burning the code")
		return
	}
	if !used {
		log.Printf("%s: registration turned down — the code is already used or expired", ClientIP(r))
		fail(w, http.StatusForbidden, "the code is already used or expired: ask for a new one")
		return
	}

	dev := &Device{
		Name:            deviceName(body.Name),
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		SignCount:       cred.Authenticator.SignCount,
		AAGUID:          cred.Authenticator.AAGUID,
		Transports:      transportStrings(cred.Transport),
		AttestationType: cred.AttestationType,
		AttestationFmt:  cred.AttestationFormat,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
	}
	id, err := p.store.Add(ctx, dev)
	if err != nil {
		log.Printf("passkey: %v (code %d burned for nothing)", err, started.code)
		fail(w, http.StatusServiceUnavailable, "the device could not be saved")
		return
	}
	dev.ID = id

	now := time.Now()
	if err := p.store.Seen(ctx, id, dev.SignCount, dev.BackupState, now); err != nil {
		log.Printf("passkey: marking the first login of device %d: %v", id, err)
	} else {
		dev.LastSeen = &now
	}

	p.session.IssueDevice(w, p.secure, id)
	log.Printf("%s: device %d added (%s)", ClientIP(r), id, dev.Name)
	writeJSON(w, http.StatusOK, deviceView(*dev))
}

// BeginLogin starts a sign-in.
func (p *Passkey) BeginLogin(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	if wait := p.session.Throttle(ClientIP(r)); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, wait %d s", int(wait.Seconds())+1))
		return
	}

	assertion, session, err := p.wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		log.Printf("passkey: starting the login: %v", err)
		fail(w, http.StatusInternalServerError, "the login could not be started")
		return
	}

	key, err := p.pending.put(ceremonyLogin, *session, 0)
	if err != nil {
		log.Printf("passkey: the login state was not saved: %v", err)
		fail(w, http.StatusInternalServerError, "the login could not be started")
		return
	}
	setCeremonyCookie(w, key, p.secure)
	writeJSON(w, http.StatusOK, assertion)
}

// FinishLogin checks the signature and issues a session.
func (p *Passkey) FinishLogin(w http.ResponseWriter, r *http.Request) {
	if !p.Enabled() {
		fail(w, http.StatusServiceUnavailable, "passkey is unavailable: there is no device database")
		return
	}
	ip := ClientIP(r)
	if wait := p.session.Throttle(ip); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, wait %d s", int(wait.Seconds())+1))
		return
	}

	key, err := r.Cookie(ceremonyCookie)
	if err != nil {
		fail(w, http.StatusBadRequest, "the login was not started")
		return
	}
	clearCeremonyCookie(w, p.secure)
	started, ok := p.pending.take(key.Value, ceremonyLogin)
	if !ok {
		fail(w, http.StatusBadRequest, "the login was not started, or it has expired: start over")
		return
	}

	var found *Device
	ctx, cancel := ctxTimeout(r.Context())
	defer cancel()

	handle, err := p.userHandle(ctx)
	if err != nil {
		dbFail(w, err, "the database could not be reached")
		return
	}

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		dev, err := p.store.ByCredential(ctx, rawID)
		if err != nil {
			return nil, err
		}
		found = dev
		return &waUser{id: handle, name: p.name, creds: []webauthn.Credential{dev.credential()}}, nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	parsed, perr := protocol.ParseCredentialRequestResponse(r)
	if err := perr; err != nil {
		p.session.Fail(ip)
		log.Printf("%s: parsing the login response: %v", ip, detail(err))
		fail(w, http.StatusBadRequest, "the authenticator answered with something unreadable")
		return
	}

	_, cred, err := p.wa.ValidatePasskeyLogin(handler, started.data, parsed)
	if err != nil {
		p.session.Fail(ip)
		log.Printf("%s: the passkey login was rejected: %v", ip, detail(err))
		fail(w, http.StatusUnauthorized, "the key was not accepted")
		return
	}

	if cred.Authenticator.CloneWarning {
		p.session.Fail(ip)
		log.Printf("%s: TURNED DOWN, the signature counter did not grow: device %d (%s), was %d, came %d — this looks like a cloned key",
			ip, found.ID, found.Name, found.SignCount, parsed.Response.AuthenticatorData.Counter)
		fail(w, http.StatusUnauthorized, "the key was not accepted")
		return
	}

	now := time.Now()
	if err := p.store.Seen(ctx, found.ID, cred.Authenticator.SignCount, cred.Flags.BackupState, now); err != nil {
		log.Printf("passkey: device %d, the login mark was not written: %v", found.ID, err)
	}

	p.session.Ok(ip)
	p.session.IssueDevice(w, p.secure, found.ID)
	log.Printf("%s: passkey login, device %d (%s)", ip, found.ID, found.Name)
	writeJSON(w, http.StatusOK, map[string]any{"device": found.ID, "name": found.Name})
}

func deviceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "device"
	}
	if utf8.RuneCountInString(name) > deviceNameMax {
		name = string([]rune(name)[:deviceNameMax])
	}
	return name
}

func detail(err error) error {
	var perr *protocol.Error
	if errors.As(err, &perr) && perr.DevInfo != "" {
		return fmt.Errorf("%s: %s", perr.Details, perr.DevInfo)
	}
	return err
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("passkey: the response was not written: %v", err)
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func dbFail(w http.ResponseWriter, err error, what string) {
	log.Printf("passkey: %v", err)
	if errors.Is(err, store.ErrUnavailable) || errors.Is(err, store.ErrClosed) {
		fail(w, http.StatusServiceUnavailable, "the database is unavailable")
		return
	}
	fail(w, http.StatusInternalServerError, what)
}

func ctxTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}
