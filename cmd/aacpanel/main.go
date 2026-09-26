package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
	"aacpanel/internal/docker"
	"aacpanel/internal/host"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/hub"
	"aacpanel/internal/notify"
	"aacpanel/internal/probes"
	"aacpanel/internal/repo"
	"aacpanel/internal/rules"
	"aacpanel/internal/store"
	"aacpanel/internal/termlink"
	"aacpanel/internal/usage"
	"aacpanel/internal/watchcfg"
	"aacpanel/web"
)

type Server struct {
	docker       *docker.Client
	auth         *auth.Service
	hub          *hub.Hub
	host         *host.Reader
	db           *store.Store
	hostName     string
	passkey      *auth.Passkey
	tokens       *auth.TokenLogin
	exec         *action.Client
	terms        *terminals
	chat         *chat.Client
	shelf        *chat.ReviewShelf
	paint        *repo.Cache
	usage        *usage.Scanner
	push         *notify.Sender
	watch        *watcher
	writer       *store.Writer
	tmpl         *template.Template
	secure       bool
	insecureSeen atomic.Bool
	termPublic   bool
	endpoints    endpoints
	// guards is poked when the map changes, so the host learns the context
	// guard of every place; nil where there is no map.
	guards chan struct{}
}

const execTimeout = 2 * time.Minute

func ownerHome() string {
	return "/home/" + hostcfg.Load().UnixUser
}

func main() {
	enrollMode := flag.Bool("enroll", false, "issue a one-time device enrolment code and exit")
	healthMode := flag.Bool("healthcheck", false, "call /healthz of our own process (for HEALTHCHECK in distroless)")
	flag.Parse()

	if *enrollMode {
		if err := runEnroll(); err != nil {
			log.Fatalf("aacpanel: %v", err)
		}
		return
	}

	if *healthMode {
		if err := runHealthcheck(); err != nil {
			fmt.Fprintf(os.Stderr, "aacpanel: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		log.Fatalf("aacpanel: %v", err)
	}
}

func runEnroll() error {
	dsn := os.Getenv("AACP_DB_DSN")
	if dsn == "" {
		return errors.New("AACP_DB_DSN is not set: enrolment codes live in the database")
	}
	db, err := store.New(dsn)
	if err != nil {
		return err
	}
	if err := db.UseOwner(os.Getenv("AACP_DB_MIGRATE_DSN")); err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.Open(ctx); err != nil {
		return fmt.Errorf("the database is unavailable, there is nowhere to write the code: %w", err)
	}

	code, expires, err := auth.NewEnrollCode(ctx, auth.NewStore(db), "console")
	if err != nil {
		return err
	}
	fmt.Println(code)
	fmt.Fprintf(os.Stderr, "Valid for %s, until %s. Burned after the first enrolment.\n",
		auth.EnrollTTL, expires.Local().Format("15:04:05"))
	return nil
}

func runHealthcheck() error {
	addr := env("AACP_ADDR", ":8776")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %s", resp.Status)
	}
	return nil
}

func run() error {
	addr := env("AACP_ADDR", ":8776")
	dockerHost := env("AACP_DOCKER", "http://socket-proxy:2375")

	idle, err := envDuration("AACP_SESSION_IDLE", auth.DefaultIdle)
	if err != nil {
		return err
	}
	maxAge, err := envDuration("AACP_SESSION_MAX", auth.DefaultAbsolute)
	if err != nil {
		return err
	}
	guard, err := auth.New(os.Getenv("AACP_SECRET"), auth.SessionTTL{Idle: idle, Absolute: maxAge})
	if err != nil {
		return err
	}
	log.Printf("session: %s without action, %s in total", idle, maxAge)

	tmpl, err := template.ParseFS(web.FS, "*.html")
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	hostName := env("AACP_HOST", hostcfg.Load().Name)

	var db *store.Store
	var writer *store.Writer
	if dsn := os.Getenv("AACP_DB_DSN"); dsn != "" {
		db, err = store.New(dsn)
		if err != nil {
			return err
		}
		if err := db.UseOwner(os.Getenv("AACP_DB_MIGRATE_DSN")); err != nil {
			return err
		}
		if err := db.UseProjectRoots(hostcfg.ProjectRoots(ownerHome())); err != nil {
			return err
		}
		writer = store.NewWriter(db, hostName)
	} else {
		log.Print("AACP_DB_DSN is not set: history and the action log are off")
	}
	defer db.Close()

	secure := env("AACP_SECURE", "1") == "1"

	var devices auth.Storage
	if db != nil {
		devices = auth.NewStore(db)
		guard.UseDevices(devices)
	} else {
		log.Print("AACP_DB_DSN is not set: there is nothing to sign in with, passkey does not work without a database")
	}
	passkey, err := auth.NewPasskey(auth.PasskeyConfig{
		RPID:    env("AACP_RP_ID", "localhost"),
		RPName:  env("AACP_RP_NAME", "aacpanel"),
		Origins: strings.Split(env("AACP_RP_ORIGINS", "http://localhost:8776"), ","),
		Secure:  secure,
	}, guard, devices)
	if err != nil {
		return err
	}

	tokens, err := auth.NewTokenLogin(env("AACP_TOKEN", ""), guard, secure)
	if err != nil {
		return err
	}
	if tokens.Enabled() {
		log.Print("AACP_TOKEN is set: token login is on alongside passkey")
	}

	rpOrigins := strings.Split(env("AACP_RP_ORIGINS", "http://localhost:8776"), ",")
	eps, err := loadEndpoints(os.Getenv("AACP_SECRET"),
		env("AACP_PUBLIC_URL", strings.TrimSpace(rpOrigins[0])), env("AACP_LAN_URL", ""), env("AACP_TS_URL", ""),
		env("AACP_LOCAL_ADDR", ""), env("AACP_LOCAL_URL", ""))
	if err != nil {
		return err
	}
	lanAddr := env("AACP_LAN_ADDR", "")
	if lanAddr != "" && eps.origin(kindLAN) == "" {
		log.Print("AACP_LAN_ADDR is set without AACP_LAN_URL: the local network listener will start, but the client will not find it")
	}
	tsAddr := env("AACP_TS_ADDR", "")
	if tsAddr != "" && eps.origin(kindTS) == "" {
		log.Print("AACP_TS_ADDR is set without AACP_TS_URL: the tailscale listener will start, but the client will not find it")
	}
	for _, e := range eps.list {
		log.Printf("panel address (%s): %s", e.Kind, e.URL)
	}

	push := notify.New(devices, env("AACP_PUSH_CONTACT", "https://"+env("AACP_RP_ID", "localhost")))

	dc := docker.New(dockerHost)
	var execClient *action.Client
	if sock := env("AACP_EXEC_SOCKET", ""); sock != "" {
		execClient = action.NewClient(sock, execTimeout)
		log.Printf("executor: %s", sock)
	} else {
		log.Print("the executor is not configured (AACP_EXEC_SOCKET is empty): actions are unavailable")
	}

	termSocket := env("AACP_TERM_SOCKET", "/run/aacpanel-exec/term.sock")
	if _, err := os.Stat(termSocket); err != nil {
		log.Printf("the session terminal is unavailable (%s): %v", termSocket, err)
		termSocket = ""
	}

	chatSocket := env("AACP_CHAT_SOCKET", "/host-state/chat/chat.sock")
	if _, err := os.Stat(chatSocket); err != nil {
		log.Printf("the session chat is unavailable (%s): %v", chatSocket, err)
		chatSocket = ""
	} else {
		log.Printf("session chat: %s", chatSocket)
	}

	// The shelf of readings answers on a socket of its own: a reading is up to
	// two hundred notes, and the chat socket takes a request that fits one read.
	reviewSocket := env("AACP_REVIEW_SOCKET", "/host-state/reviews/review.sock")
	if _, err := os.Stat(reviewSocket); err != nil {
		log.Printf("the shelf of readings is unavailable (%s): %v", reviewSocket, err)
		reviewSocket = ""
	} else {
		log.Printf("shelf of readings: %s", reviewSocket)
	}

	usageSocket := env("AACP_USAGE_SOCKET", "/host-state/usage/usage.sock")
	if _, err := os.Stat(usageSocket); err != nil {
		log.Printf("usage collection is unavailable (%s): %v", usageSocket, err)
		usageSocket = ""
	} else {
		log.Printf("usage collection: %s", usageSocket)
	}

	termPublic := env("AACP_TERM_PUBLIC", "0") == "1"
	if termPublic {
		if termSocket == "" {
			log.Print("the terminal is on for the public listener, but there is no terminal socket — " +
				"its endpoints will answer that the terminal is not configured")
		} else {
			log.Print("the session terminal is reachable from outside too: AACP_TERM_PUBLIC=1")
		}
	}

	srv := &Server{
		docker:     dc,
		auth:       guard,
		hub:        hub.New(dc, writer),
		host:       host.NewReader(env("AACP_STATE", "/host-state/state.json")),
		db:         db,
		hostName:   hostName,
		passkey:    passkey,
		tokens:     tokens,
		exec:       execClient,
		terms:      newTerminals(termlink.NewClient(termSocket)),
		chat:       chat.New(chatSocket),
		shelf:      &chat.ReviewShelf{Socket: reviewSocket},
		paint:      repo.NewCache(repo.CacheEntries),
		usage:      usage.NewScanner(usage.New(usageSocket), db),
		push:       push,
		writer:     writer,
		tmpl:       tmpl,
		secure:     secure,
		termPublic: termPublic,
		endpoints:  eps,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if db != nil {
		go func() {
			if err := db.Run(ctx); err != nil {
				log.Fatalf("aacpanel: %v", err)
			}
		}()
		go writer.Run(ctx)
		go recordHost(ctx, srv.host, writer)
		go watchcfg.Apply(ctx, db)
		go rules.NewEngine(db, hostName).Run(ctx)
		go probes.New(db).Run(ctx)
		push.UseViews(db.SessionViews())
		push.UseJournal(db.PushJournal())
		go push.Run(ctx)
		srv.watch = newWatcher(srv, db.PushJournal())
		go srv.watch.Run(ctx)
		srv.guards = make(chan struct{}, 1)
		go srv.runGuards(ctx)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	tree, err := dc.Tree(probeCtx, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("docker is unavailable (%s): %w", dockerHost, err)
	}
	log.Printf("docker %s: %d containers, %d running", dockerHost, tree.Total, tree.Running)

	go srv.hub.Run(ctx)
	go srv.usage.RunDaily(ctx)

	mux := srv.handler(srv.publicGate())

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutCtx)
	}()

	if localAddr := env("AACP_LOCAL_ADDR", ""); localAddr != "" {
		local, err := srv.localServer(localAddr)
		if err != nil {
			return err
		}
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			local.Shutdown(shutCtx)
		}()
		go func() {
			log.Printf("the local panel on %s: no login, with the session terminal", localAddr)
			if err := local.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("the local panel died: %v", err)
			}
		}()
	}

	if lanAddr != "" {
		lan, err := srv.lanServer(lanAddr, env("AACP_TLS_CERT", ""), env("AACP_TLS_KEY", ""))
		if err != nil {
			return err
		}
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			lan.Shutdown(shutCtx)
		}()
		go func() {
			log.Printf("the local network listener on %s: with login, TLS from %s", lanAddr, env("AACP_TLS_CERT", ""))
			if err := lan.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("the local network listener died: %v", err)
			}
		}()
	}

	if tsAddr != "" {
		ts := srv.tsServer(tsAddr)
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ts.Shutdown(shutCtx)
		}()
		go func() {
			log.Printf("the tailscale listener on %s: with login, TLS is held by tailscale serve", tsAddr)
			if err := ts.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("the tailscale listener died: %v", err)
			}
		}()
	}

	log.Printf("listening on %s", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("stopped")
	return nil
}

func recordHost(ctx context.Context, reader *host.Reader, w *store.Writer) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()

	for {
		payload, err := reader.JSON()
		if err == nil {
			w.AgentSnapshot(payload)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w (a form like 30m or 12h is expected)", key, raw, err)
	}
	return d, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" {
			log.Printf("%s %s %s %s", auth.ClientIP(r), r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
