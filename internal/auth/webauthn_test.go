package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"aacpanel/internal/notify"
)

const (
	testRPID   = "localhost"
	testOrigin = "http://localhost"
	testSecret = "0123456789abcdef0123456789abcdef"
)

type memDevices struct {
	mu    sync.Mutex
	next  int64
	list  []Device
	codes []memCode
	subs  []notify.Subscription
	vapid []byte
	fail  error
}

type memCode struct {
	id      int64
	hash    string
	expires time.Time
	used    bool
}

var testHandle = []byte("0123456789abcdef0123456789abcdef")

func (m *memDevices) UserHandle(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	return testHandle, nil
}

func (m *memDevices) AddCode(_ context.Context, hash, source string, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.codes = append(m.codes, memCode{id: int64(len(m.codes) + 1), hash: hash, expires: expires})
	return nil
}

func (m *memDevices) ActiveCodes(context.Context) ([]EnrollCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	var out []EnrollCode
	for _, c := range m.codes {
		if !c.used && time.Now().Before(c.expires) {
			out = append(out, EnrollCode{ID: c.id, Hash: c.hash})
		}
	}
	return out, nil
}

func (m *memDevices) UseCode(_ context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	for i := range m.codes {
		if m.codes[i].id == id && !m.codes[i].used && time.Now().Before(m.codes[i].expires) {
			m.codes[i].used = true
			return true, nil
		}
	}
	return false, nil
}

func (m *memDevices) expireCodes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.codes {
		m.codes[i].expires = time.Now().Add(-time.Minute)
	}
}

func (m *memDevices) Add(_ context.Context, d *Device) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return 0, m.fail
	}
	m.next++
	cp := *d
	cp.ID = m.next
	cp.CreatedAt = time.Now()
	m.list = append(m.list, cp)
	return cp.ID, nil
}

func (m *memDevices) ByCredential(_ context.Context, credentialID []byte) (*Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	for i := range m.list {
		if m.list[i].RevokedAt == nil && bytes.Equal(m.list[i].CredentialID, credentialID) {
			cp := m.list[i]
			return &cp, nil
		}
	}
	return nil, ErrNoDevice
}

func (m *memDevices) List(context.Context) ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	return m.pick(func(d Device) bool { return d.RevokedAt == nil }), nil
}

func (m *memDevices) Revoked(context.Context) ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := m.pick(func(d Device) bool { return d.RevokedAt != nil })
	slices.SortFunc(out, func(a, b Device) int { return b.RevokedAt.Compare(*a.RevokedAt) })
	return out, nil
}

func (m *memDevices) pick(keep func(Device) bool) []Device {
	out := []Device{}
	for _, d := range m.list {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

func (m *memDevices) Exists(_ context.Context, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	return slices.ContainsFunc(m.list, func(d Device) bool {
		return d.ID == id && d.RevokedAt == nil
	}), nil
}

func (m *memDevices) Seen(_ context.Context, id int64, signCount uint32, backupState bool, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i := range m.list {
		if m.list[i].ID == id {
			m.list[i].SignCount = signCount
			m.list[i].BackupState = backupState
			m.list[i].LastSeen = &at
			return nil
		}
	}
	return ErrNoDevice
}

func (m *memDevices) Rename(_ context.Context, id int64, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i := range m.list {
		if m.list[i].ID == id {
			m.list[i].Name = name
			return nil
		}
	}
	return ErrNoDevice
}

func (m *memDevices) Revoke(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i := range m.list {
		if m.list[i].ID == id && m.list[i].RevokedAt == nil {
			now := time.Now()
			m.list[i].RevokedAt = &now
			m.list[i].CredentialID = nil
			m.list[i].PublicKey = nil
			m.subs = slices.DeleteFunc(m.subs, func(s notify.Subscription) bool {
				return s.Device == id
			})
			return nil
		}
	}
	return ErrNoDevice
}

func (m *memDevices) get(t *testing.T, id int64) Device {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.list {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("device %d is not in the storage", id)
	return Device{}
}

type stand struct {
	srv     *httptest.Server
	guard   *Service
	passkey *Passkey
	devices Storage
	mem     *memDevices
}

func newStand(t *testing.T) *stand {
	t.Helper()
	mem := &memDevices{}
	s := newStandWith(t, mem)
	s.mem = mem
	return s
}

func newStandWith(t *testing.T, devices Storage) *stand {
	t.Helper()

	guard, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("session service: %v", err)
	}

	guard.UseDevices(devices)

	pk, err := NewPasskey(PasskeyConfig{
		RPID:    testRPID,
		RPName:  "aacpanel",
		Origins: []string{testOrigin},
		Secure:  false,
	}, guard, devices)
	if err != nil {
		t.Fatalf("passkey: %v", err)
	}

	guarded := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !guard.Authorized(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		})
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/enroll/code", guarded(pk.IssueCode))
	mux.Handle("GET /api/push/subscription", guarded(pk.Subscription))
	mux.Handle("POST /api/push/subscription", guarded(pk.Subscribe))
	mux.Handle("DELETE /api/push/subscription", guarded(pk.Unsubscribe))
	mux.HandleFunc("POST /auth/passkey/register/begin", pk.BeginRegister)
	mux.HandleFunc("POST /auth/passkey/register/finish", pk.FinishRegister)
	mux.HandleFunc("POST /auth/passkey/login/begin", pk.BeginLogin)
	mux.HandleFunc("POST /auth/passkey/login/finish", pk.FinishLogin)
	mux.Handle("GET /api/devices", guarded(pk.ListDevices))
	mux.Handle("PATCH /api/devices/{id}", guarded(pk.RenameDevice))
	mux.Handle("DELETE /api/devices/{id}", guarded(pk.RevokeDevice))
	mux.HandleFunc("GET /protected", func(w http.ResponseWriter, r *http.Request) {
		if !guard.Authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Write([]byte("ok"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &stand{srv: srv, guard: guard, passkey: pk, devices: devices}
}

func (s *stand) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func (s *stand) do(t *testing.T, c *http.Client, method, path string, body []byte) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func userHandleOf(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out struct {
		PublicKey struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parsing the answer: %v (%s)", err, raw)
	}
	handle, err := base64.RawURLEncoding.DecodeString(out.PublicKey.User.ID)
	if err != nil || len(handle) == 0 {
		t.Fatalf("the answer carries no user handle: %v (%s)", err, raw)
	}
	return handle
}

func challengeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var out struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parsing the answer: %v (%s)", err, raw)
	}
	if out.PublicKey.Challenge == "" {
		t.Fatalf("the answer carries no challenge: %s", raw)
	}
	return out.PublicKey.Challenge
}

func (s *stand) deviceCount(t *testing.T) int {
	t.Helper()
	list, err := s.devices.List(context.Background())
	if err != nil {
		t.Fatalf("list of devices: %v", err)
	}
	return len(list)
}

func (s *stand) enrollCode(t *testing.T) string {
	t.Helper()
	code, _, err := NewEnrollCode(context.Background(), s.devices, "console")
	if err != nil {
		t.Fatalf("issuing a code: %v", err)
	}
	return code
}

func (s *stand) registerFirst(t *testing.T, key *softKey, name string) (*http.Client, int64) {
	t.Helper()
	c, code, body := s.register(t, s.client(t), key, name, s.enrollCode(t))
	if code != http.StatusOK {
		t.Fatalf("registration: %d %s", code, body)
	}
	var dev deviceJSON
	if err := json.Unmarshal(body, &dev); err != nil {
		t.Fatalf("parsing the device: %v (%s)", err, body)
	}
	return c, dev.ID
}

func (s *stand) register(t *testing.T, c *http.Client, key *softKey, name, enroll string) (*http.Client, int, []byte) {
	t.Helper()
	begin, _ := json.Marshal(registerBeginRequest{Code: enroll})
	code, body := s.do(t, c, "POST", "/auth/passkey/register/begin", begin)
	if code != http.StatusOK {
		return c, code, body
	}

	key.handle = userHandleOf(t, body)
	cred := key.register(t, testRPID, challengeOf(t, body), testOrigin)
	payload, _ := json.Marshal(registerRequest{Name: name, Credential: cred})
	code, body = s.do(t, c, "POST", "/auth/passkey/register/finish", payload)
	return c, code, body
}

func (s *stand) login(t *testing.T, key *softKey, count uint32) (*http.Client, int, []byte) {
	t.Helper()
	c := s.client(t)
	code, body := s.do(t, c, "POST", "/auth/passkey/login/begin", nil)
	if code != http.StatusOK {
		t.Fatalf("beginning of login: %d %s", code, body)
	}
	assertion := key.login(t, testRPID, challengeOf(t, body), testOrigin, key.handle, count)
	code, body = s.do(t, c, "POST", "/auth/passkey/login/finish", assertion)
	return c, code, body
}

func TestRegisterAndLogin(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)

	c, id := s.registerFirst(t, key, "Pixel")
	if id == 0 {
		t.Fatal("the device got no number")
	}
	if dev := s.mem.get(t, id); dev.Name != "Pixel" || len(dev.PublicKey) == 0 {
		t.Fatalf("the device was written wrong: %+v", dev)
	}

	if code, body := s.do(t, c, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the session after registration does not let through: %d %s", code, body)
	}

	c2, code, body := s.login(t, key, key.count+1)
	if code != http.StatusOK {
		t.Fatalf("passkey login: %d %s", code, body)
	}
	if code, body := s.do(t, c2, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the session after login does not let through: %d %s", code, body)
	}

	dev := s.mem.get(t, id)
	if dev.SignCount != key.count+1 {
		t.Fatalf("the signature counter was not written: %d in the database, want %d", dev.SignCount, key.count+1)
	}
	if dev.LastSeen == nil {
		t.Fatal("the time of the last login was not written")
	}
}

func TestLoginRejectsStaleSignCount(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)
	_, id := s.registerFirst(t, key, "Pixel")

	if _, code, body := s.login(t, key, 7); code != http.StatusOK {
		t.Fatalf("an ordinary login: %d %s", code, body)
	}

	c, code, body := s.login(t, key, 7)
	if code != http.StatusUnauthorized {
		t.Fatalf("a login whose counter did not grow went through: %d %s", code, body)
	}
	if code, _ := s.do(t, c, "GET", "/protected", nil); code != http.StatusUnauthorized {
		t.Fatalf("a session was issued after the refusal anyway: %d", code)
	}
	if dev := s.mem.get(t, id); dev.SignCount != 7 {
		t.Fatalf("the counter was spoiled by a refused login: %d", dev.SignCount)
	}
}

func TestLoginRejectsForeignKey(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)
	s.registerFirst(t, key, "Pixel")

	other := newSoftKey(t)
	other.handle = key.handle
	if _, code, _ := s.login(t, other, 1); code != http.StatusUnauthorized {
		t.Fatalf("a foreign key was let through: %d", code)
	}

	fake := newSoftKey(t)
	fake.credID = key.credID
	fake.handle = key.handle
	if _, code, _ := s.login(t, fake, 99); code != http.StatusUnauthorized {
		t.Fatalf("a forged signature was accepted: %d", code)
	}
}

func TestRegisterRequiresCode(t *testing.T) {
	s := newStand(t)

	for _, code := range []string{"", "QQQQQ-QQQQQ", "not-a-code"} {
		begin, _ := json.Marshal(registerBeginRequest{Code: code})
		status, body := s.do(t, s.client(t), "POST", "/auth/passkey/register/begin", begin)
		if status != http.StatusForbidden {
			t.Fatalf("code %q was let into the beginning of the ceremony: %d %s", code, status, body)
		}
	}
	if n := s.deviceCount(t); n != 0 {
		t.Fatalf("a device appeared without a code: %d", n)
	}
}

func TestEnrollCodeIsSingleUse(t *testing.T) {
	s := newStand(t)
	code := s.enrollCode(t)

	if _, status, body := s.register(t, s.client(t), newSoftKey(t), "first", code); status != http.StatusOK {
		t.Fatalf("registration by code: %d %s", status, body)
	}
	if _, status, body := s.register(t, s.client(t), newSoftKey(t), "second", code); status != http.StatusForbidden {
		t.Fatalf("the code worked twice: %d %s", status, body)
	}
	if n := s.deviceCount(t); n != 1 {
		t.Fatalf("%d devices in the database, expected one", n)
	}
}

func TestAbandonedCeremonyKeepsCode(t *testing.T) {
	s := newStand(t)
	code := s.enrollCode(t)

	begin, _ := json.Marshal(registerBeginRequest{Code: code})
	if status, body := s.do(t, s.client(t), "POST", "/auth/passkey/register/begin", begin); status != http.StatusOK {
		t.Fatalf("beginning of registration: %d %s", status, body)
	}
	if _, status, body := s.register(t, s.client(t), newSoftKey(t), "phone", code); status != http.StatusOK {
		t.Fatalf("the code burned out on an abandoned ceremony: %d %s", status, body)
	}
}

func TestEnrollCodeExpires(t *testing.T) {
	s := newStand(t)
	code := s.enrollCode(t)
	s.mem.expireCodes()

	if _, status, body := s.register(t, s.client(t), newSoftKey(t), "phone", code); status != http.StatusForbidden {
		t.Fatalf("an expired code was accepted: %d %s", status, body)
	}
}

func TestEnrollBruteForceThrottled(t *testing.T) {
	s := newStand(t)
	good := s.enrollCode(t)
	c := s.client(t)

	for i := range 5 {
		begin, _ := json.Marshal(registerBeginRequest{Code: "ZZZZZ-ZZZZZ"})
		if status, _ := s.do(t, c, "POST", "/auth/passkey/register/begin", begin); status != http.StatusForbidden {
			t.Fatalf("attempt %d: expected a refusal, got %d", i+1, status)
		}
	}
	begin, _ := json.Marshal(registerBeginRequest{Code: good})
	if status, body := s.do(t, c, "POST", "/auth/passkey/register/begin", begin); status != http.StatusTooManyRequests {
		t.Fatalf("brute force did not run into throttling: %d %s", status, body)
	}
}

func TestSecondDeviceViaPanelCode(t *testing.T) {
	s := newStand(t)
	first := newSoftKey(t)
	c1, firstID := s.registerFirst(t, first, "first")

	if status, _ := s.do(t, s.client(t), "POST", "/api/enroll/code", nil); status != http.StatusUnauthorized {
		t.Fatalf("a code was issued without a session: %d", status)
	}

	status, body := s.do(t, c1, "POST", "/api/enroll/code", nil)
	if status != http.StatusOK {
		t.Fatalf("issuing a code from the panel: %d %s", status, body)
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Code == "" {
		t.Fatalf("the answer carries no code: %v (%s)", err, body)
	}

	second := newSoftKey(t)
	_, status, body = s.register(t, s.client(t), second, "second", out.Code)
	if status != http.StatusOK {
		t.Fatalf("registration of the second device: %d %s", status, body)
	}

	if _, status, _ := s.login(t, first, first.count+1); status != http.StatusOK {
		t.Fatalf("the first device stopped being able to log in: %d", status)
	}
	if _, status, _ := s.login(t, second, second.count+1); status != http.StatusOK {
		t.Fatalf("the second device cannot log in: %d", status)
	}
	if code, _ := s.do(t, c1, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the session of the first device broke: %d", code)
	}
	if n := s.deviceCount(t); n != 2 {
		t.Fatalf("%d devices in the database, expected two", n)
	}
	_ = firstID
}

func TestCodeNormalization(t *testing.T) {
	s := newStand(t)
	code := s.enrollCode(t)

	messy := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	messy = strings.ReplaceAll(messy, "0", "o")
	messy = strings.ReplaceAll(messy, "1", "l")

	if _, status, body := s.register(t, s.client(t), newSoftKey(t), "phone", "  "+messy+"  "); status != http.StatusOK {
		t.Fatalf("a code with typos was rejected: %d %s", status, body)
	}
}

func TestRevokeClosesOnlyItsOwnDevice(t *testing.T) {
	s := newStand(t)

	first := newSoftKey(t)
	_, firstID := s.registerFirst(t, first, "Pixel")

	second := newSoftKey(t)
	second.handle = first.handle
	second.count = 3
	secondID, err := s.devices.Add(context.Background(), &Device{
		Name:            "old phone",
		CredentialID:    second.credID,
		PublicKey:       second.coseKey(),
		SignCount:       second.count,
		AAGUID:          second.aaguid,
		Transports:      []string{"internal"},
		AttestationType: "none",
		AttestationFmt:  "none",
	})
	if err != nil {
		t.Fatalf("the second device: %v", err)
	}

	c1, code, body := s.login(t, first, first.count+1)
	if code != http.StatusOK {
		t.Fatalf("login with the first device: %d %s", code, body)
	}
	c2, code, body := s.login(t, second, second.count+1)
	if code != http.StatusOK {
		t.Fatalf("login with the second device: %d %s", code, body)
	}

	if code, body := s.do(t, c2, "DELETE", fmt.Sprintf("/api/devices/%d", firstID), nil); code != http.StatusOK {
		t.Fatalf("revoking a device: %d %s", code, body)
	}

	if code, _ := s.do(t, c1, "GET", "/protected", nil); code != http.StatusUnauthorized {
		t.Fatalf("a revoked device keeps getting through: %d", code)
	}
	if code, body := s.do(t, c2, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the revocation touched another device: %d %s", code, body)
	}

	if _, code, _ := s.login(t, first, first.count+5); code != http.StatusUnauthorized {
		t.Fatalf("a revoked key was let through again: %d", code)
	}

	code, body = s.do(t, c2, "GET", "/api/devices", nil)
	if code != http.StatusOK {
		t.Fatalf("list of devices: %d %s", code, body)
	}
	var list struct {
		Devices []deviceJSON `json:"devices"`
		Revoked []deviceJSON `json:"revoked"`
		Current int64        `json:"current"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("parsing the list: %v (%s)", err, body)
	}
	if len(list.Devices) != 1 || list.Devices[0].ID != secondID {
		t.Fatalf("the list holds the wrong thing: %+v", list.Devices)
	}
	if list.Current != secondID {
		t.Fatalf("the current device was determined wrong: %d", list.Current)
	}

	if len(list.Revoked) != 1 || list.Revoked[0].ID != firstID {
		t.Fatalf("the revoked one is missing from the answer: %+v", list.Revoked)
	}
	if list.Revoked[0].RevokedAt == nil {
		t.Error("the revoked one has no timestamp")
	}
	if list.Revoked[0].Name == "" {
		t.Error("the name of the revoked one was lost — nobody recognises it by number")
	}
}

func TestRenameDevice(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)
	c, id := s.registerFirst(t, key, "Pixel")

	body, _ := json.Marshal(map[string]string{"name": "  work phone  "})
	if code, resp := s.do(t, c, "PATCH", fmt.Sprintf("/api/devices/%d", id), body); code != http.StatusOK {
		t.Fatalf("renaming: %d %s", code, resp)
	}
	if dev := s.mem.get(t, id); dev.Name != "work phone" {
		t.Fatalf("the name was not saved: %q", dev.Name)
	}

	if code, _ := s.do(t, c, "PATCH", "/api/devices/999", body); code != http.StatusNotFound {
		t.Fatalf("renaming one that does not exist: %d", code)
	}
}

func TestSessionWithDeviceNeedsStore(t *testing.T) {
	guard, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	w := httptest.NewRecorder()
	guard.IssueDevice(w, false, 42)
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	if guard.Authorized(r) {
		t.Fatal("a session with a device went through without a device storage")
	}

	devices := &memDevices{}
	guard.UseDevices(devices)
	if guard.Authorized(r) {
		t.Fatal("a session of a device that does not exist went through")
	}
}

func TestDeviceCheckSurvivesDatabaseOutage(t *testing.T) {
	s := newStand(t)
	key := newSoftKey(t)
	c, id := s.registerFirst(t, key, "Pixel")

	if code, _ := s.do(t, c, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("the first check did not pass")
	}

	s.mem.mu.Lock()
	s.mem.fail = errors.New("database unavailable")
	s.mem.mu.Unlock()

	if code, _ := s.do(t, c, "GET", "/protected", nil); code != http.StatusOK {
		t.Fatalf("a database that is down threw the session out")
	}

	w := httptest.NewRecorder()
	s.guard.IssueDevice(w, false, id+100)
	r := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	if s.guard.Authorized(r) {
		t.Fatal("an unchecked device went through while the database is down")
	}
}

func TestPendingEvictsOldestInsteadOfRefusing(t *testing.T) {
	p := newPendingStore()
	for range pendingLimit * 2 {
		if _, err := p.put(ceremonyLogin, webauthn.SessionData{}, 0); err != nil {
			t.Fatalf("the ceremony did not start: %v", err)
		}
	}

	key, err := p.put(ceremonyLogin, webauthn.SessionData{Challenge: "the last one"}, 0)
	if err != nil {
		t.Fatalf("the last ceremony did not start: %v", err)
	}
	if len(p.items) > pendingLimit {
		t.Fatalf("the map of ceremonies grew past its limit: %d", len(p.items))
	}
	item, ok := p.take(key, ceremonyLogin)
	if !ok || item.data.Challenge != "the last one" {
		t.Fatalf("the freshest ceremony was lost: %v %+v", ok, item)
	}
	if _, ok := p.take(key, ceremonyLogin); ok {
		t.Fatal("the ceremony state was reused")
	}
}

func (m *memDevices) Subscriptions(context.Context) ([]notify.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	return slices.Clone(m.subs), nil
}

func (m *memDevices) SaveSubscription(_ context.Context, sub notify.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i := range m.subs {
		if m.subs[i].Device == sub.Device {
			m.subs[i] = sub
			return nil
		}
	}
	m.subs = append(m.subs, sub)
	return nil
}

func (m *memDevices) DropSubscription(_ context.Context, device int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.subs = slices.DeleteFunc(m.subs, func(s notify.Subscription) bool { return s.Device == device })
	return nil
}

func (m *memDevices) SubscriptionSent(context.Context, int64, time.Time) error { return m.fail }
func (m *memDevices) SubscriptionFailed(context.Context, int64, string) error  { return m.fail }
func (m *memDevices) PushKeys(context.Context) ([]byte, error)                 { return m.vapid, m.fail }
func (m *memDevices) SavePushKeys(_ context.Context, private, _ []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if m.vapid == nil {
		m.vapid = private
	}
	return nil
}

func TestRegistrationCountsAsFirstLogin(t *testing.T) {
	s := newStand(t)
	_, id := s.registerFirst(t, newSoftKey(t), "new laptop")

	list, err := s.mem.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range list {
		if d.ID != id {
			continue
		}
		found = true
		if d.LastSeen == nil {
			t.Error("a device that was just added counts as never having logged in")
		}
	}
	if !found {
		t.Fatalf("the device that was added is missing from the list: %+v", list)
	}
}
