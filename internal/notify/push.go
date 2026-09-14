package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	queueSize   = 256
	workers     = 4
	sendTimeout = 15 * time.Second
	attempts    = 3
	retryPause  = 3 * time.Second
	retryCap    = time.Minute
	ttlNormal   = time.Hour
	ttlCritical = 6 * time.Hour
	topicMax    = 32
)

// Severity is the importance of a notification.
type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

// Message is a notification as the person will see it.
type Message struct {
	Title    string
	Body     string
	Tag      string
	Severity Severity
	Session  string
}

func (m Message) valid() error {
	if strings.TrimSpace(m.Title) == "" {
		return errors.New("a push with no title")
	}
	if strings.TrimSpace(m.Body) == "" {
		return errors.New("a push with no body: a notification that has to be opened to be read is useless")
	}
	return nil
}

func (m Message) ttl() time.Duration {
	if m.Severity == Critical {
		return ttlCritical
	}
	return ttlNormal
}

// screen is the panel address a tap on the notification opens: the screen of
// the session the push is about, and nothing for the rest — those only bring
// the panel up.
func (m Message) screen() string {
	if m.Session == "" {
		return ""
	}
	return "/app?session=" + url.QueryEscape(m.Session)
}

func (m Message) urgency() string {
	switch m.Severity {
	case Critical:
		return "high"
	case Info:
		return "low"
	default:
		return "normal"
	}
}

// Subscription is a device subscription as the browser returned it.
type Subscription struct {
	Device   int64
	Endpoint string
	P256dh   []byte
	Auth     []byte
}

// Validate checks what came from the browser.
func (s Subscription) Validate() error {
	if _, err := origin(s.Endpoint); err != nil {
		return err
	}
	if len(s.P256dh) != publicKeyLen {
		return fmt.Errorf("the device key is %d bytes long, %d expected", len(s.P256dh), publicKeyLen)
	}
	if len(s.Auth) != authLen {
		return fmt.Errorf("the subscription secret is %d bytes long, %d expected", len(s.Auth), authLen)
	}
	return nil
}

// Store is the storage of subscriptions and of the VAPID pair.
type Store interface {
	Subscriptions(ctx context.Context) ([]Subscription, error)
	DropSubscription(ctx context.Context, device int64) error
	SubscriptionSent(ctx context.Context, device int64, at time.Time) error
	SubscriptionFailed(ctx context.Context, device int64, reason string) error
	PushKeys(ctx context.Context) (private []byte, err error)
	SavePushKeys(ctx context.Context, private, public []byte) error
}

// Sender sends notifications.
type Sender struct {
	store   Store
	contact string
	client  *http.Client
	queue   chan Message
	views   Views
	journal Journal

	ready chan struct{}
	once  sync.Once
	keys  *Keys
}

// New builds a sender for the given owner contact.
func New(store Store, contact string) *Sender {
	return &Sender{
		store:   store,
		contact: contact,
		client:  &http.Client{Timeout: sendTimeout},
		queue:   make(chan Message, queueSize),
		ready:   make(chan struct{}),
	}
}

// Run brings up the keys and serves the queue.
func (s *Sender) Run(ctx context.Context) {
	if err := s.loadKeys(ctx); err != nil {
		if ctx.Err() == nil {
			log.Printf("notify: the VAPID keys did not arrive, pushes are off: %v", err)
		}
		return
	}

	var done sync.WaitGroup
	for range workers {
		done.Add(1)
		go func() {
			defer done.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case m := <-s.queue:
					s.deliver(ctx, m)
				}
			}
		}()
	}
	done.Wait()
}

func (s *Sender) loadKeys(ctx context.Context) error {
	if s.Ready() {
		return nil
	}
	delay := 2 * time.Second
	for {
		keys, err := s.fetchKeys(ctx)
		if err == nil {
			s.keys = keys
			s.once.Do(func() {
				close(s.ready)
				log.Printf("notify: pushes are ready, key %s…", keys.PublicBase64()[:12])
			})
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("notify: %v; retrying in %s", err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > 30*time.Second {
			delay = 30 * time.Second
		}
	}
}

func (s *Sender) fetchKeys(ctx context.Context) (*Keys, error) {
	der, err := s.store.PushKeys(ctx)
	if err != nil {
		return nil, err
	}
	if len(der) > 0 {
		return LoadKeys(der)
	}

	keys, err := NewKeys()
	if err != nil {
		return nil, err
	}
	if err := s.store.SavePushKeys(ctx, keys.PrivateDER, keys.Public); err != nil {
		return nil, err
	}
	der, err = s.store.PushKeys(ctx)
	if err != nil {
		return nil, err
	}
	return LoadKeys(der)
}

// PublicKey returns the applicationServerKey for the frontend.
func (s *Sender) PublicKey() string {
	select {
	case <-s.ready:
		return s.keys.PublicBase64()
	default:
		return ""
	}
}

// Ready reports whether subscribing and sending are possible yet.
func (s *Sender) Ready() bool {
	select {
	case <-s.ready:
		return true
	default:
		return false
	}
}

// Send queues a notification and returns at once.
func (s *Sender) Send(m Message) {
	if err := m.valid(); err != nil {
		log.Printf("notify: %v (%q)", err, m.Title)
		return
	}
	select {
	case s.queue <- m:
	default:
		log.Printf("notify: the push queue is full, the notification was dropped: %s", m.Title)
	}
}

func (s *Sender) deliver(ctx context.Context, m Message) {
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	subs, err := s.store.Subscriptions(listCtx)
	cancel()
	if err != nil {
		log.Printf("notify: the subscription list: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	fields := map[string]any{
		"title":    m.Title,
		"body":     m.Body,
		"tag":      m.Tag,
		"severity": string(m.Severity),
		"ts":       time.Now().Unix(),
	}
	if screen := m.screen(); screen != "" {
		fields["url"] = screen
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		log.Printf("notify: assembling the notification: %v", err)
		return
	}

	seen := s.watching(ctx, m)
	for _, sub := range subs {
		if seen[sub.Device] {
			log.Printf("notify: device %d is looking at session %s — not sending %q", sub.Device, m.Session, m.Title)
			continue
		}
		s.deliverTo(ctx, sub, m, payload)
	}
}

func (s *Sender) deliverTo(ctx context.Context, sub Subscription, m Message, payload []byte) {
	for attempt := 1; attempt <= attempts; attempt++ {
		status, retryAfter, err := s.post(ctx, sub, m, payload)
		switch {
		case err != nil:
			s.note(ctx, sub.Device, fmt.Sprintf("sending: %v", err))
			log.Printf("notify: device %d, attempt %d: %v", sub.Device, attempt, err)

		case status == http.StatusNotFound || status == http.StatusGone:
			log.Printf("notify: the subscription of device %d is dead (%d), removing it", sub.Device, status)
			dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := s.store.DropSubscription(dropCtx, sub.Device); err != nil {
				log.Printf("notify: deleting the subscription of %d: %v", sub.Device, err)
			}
			s.bury(dropCtx, sub.Device, status)
			cancel()
			return

		case status == http.StatusTooManyRequests || status >= 500:
			pause := retryPause
			if retryAfter > 0 {
				pause = min(retryAfter, retryCap)
			}
			log.Printf("notify: device %d, answer %d, retrying in %s", sub.Device, status, pause)
			s.note(ctx, sub.Device, fmt.Sprintf("answer %d", status))
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
			continue

		case status >= 200 && status < 300:
			sentCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := s.store.SubscriptionSent(sentCtx, sub.Device, time.Now()); err != nil {
				log.Printf("notify: marking the delivery to %d: %v", sub.Device, err)
			}
			cancel()
			return

		default:
			log.Printf("notify: device %d, the push service answered %d — a retry will not help", sub.Device, status)
			s.note(ctx, sub.Device, fmt.Sprintf("answer %d", status))
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryPause):
		}
	}
	log.Printf("notify: device %d, the delivery failed in %d attempts", sub.Device, attempts)
}

func (s *Sender) post(ctx context.Context, sub Subscription, m Message, payload []byte) (int, time.Duration, error) {
	if !s.Ready() {
		return 0, 0, errors.New("the VAPID keys are not ready")
	}
	body, err := encrypt(payload, sub)
	if err != nil {
		return 0, 0, err
	}
	auth, err := s.keys.authorization(sub.Endpoint, s.contact, time.Now())
	if err != nil {
		return 0, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(m.ttl().Seconds())))
	req.Header.Set("Urgency", m.urgency())
	if topic := topicOf(m.Tag); topic != "" {
		req.Header.Set("Topic", topic)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	var retry time.Duration
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			retry = time.Duration(secs) * time.Second
		}
	}
	return resp.StatusCode, retry, nil
}

const domainPush = "push"

// UseJournal plugs in the memory of reasons: a subscription the push service
// declared dead is written down there.
func (s *Sender) UseJournal(j Journal) { s.journal = j }

// bury writes down that the push service declared the subscription dead. The
// row itself is removed right after, and the screen of the device still says
// "on" — the browser side is alive — so this note is the only trace of when
// and why a device stopped getting pushes. It is kept under the key of the
// device, and the next death of the same device overwrites it; the tracker
// leaves it alone, as nobody reports on this domain.
func (s *Sender) bury(ctx context.Context, device int64, status int) {
	if s.journal == nil {
		return
	}
	e := Event{
		Key:      "push:" + strconv.FormatInt(device, 10),
		Domain:   domainPush,
		Title:    fmt.Sprintf("device %d lost its push subscription", device),
		Body:     fmt.Sprintf("the push service answered %d, the subscription was removed", status),
		Severity: Warning,
	}
	if err := s.journal.Raise(ctx, e); err != nil {
		log.Printf("notify: the death of the subscription of %d was not written down: %v", device, err)
	}
}

func (s *Sender) note(ctx context.Context, device int64, reason string) {
	noteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.SubscriptionFailed(noteCtx, device, reason); err != nil {
		log.Printf("notify: marking the failure of %d: %v", device, err)
	}
}

func topicOf(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
		if b.Len() >= topicMax {
			break
		}
	}
	return b.String()
}
