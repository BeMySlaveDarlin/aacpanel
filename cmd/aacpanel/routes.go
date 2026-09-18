package main

import (
	"net/http"
	"strings"

	"aacpanel/internal/auth"
)

type gate struct {
	page   func(http.HandlerFunc) http.Handler
	stream func(http.HandlerFunc) http.Handler
	local  bool
	kind   string
	term   bool
}

func (s *Server) publicGate() gate {
	return gate{page: s.protect, stream: s.protectStream, term: s.termPublic, kind: kindPublic}
}

func (s *Server) localGate() gate {
	plain := func(h http.HandlerFunc) http.Handler { return h }
	return gate{page: plain, stream: plain, local: true, term: true, kind: kindLocal}
}

func (s *Server) handler(g gate) http.Handler {
	return s.cors(s.noteScheme(s.routes(g)))
}

func (s *Server) noteScheme(next http.Handler) http.Handler {
	if s.secure {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			s.insecureSeen.Store(true)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes(g gate) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /probe", s.probe(g.kind))
	mux.HandleFunc("OPTIONS /probe", s.probe(g.kind))
	mux.Handle("GET /api/endpoints", g.page(s.apiEndpoints(g.kind)))
	mux.Handle("GET /api/session/token", g.page(s.apiSessionToken))
	mux.Handle("GET /static/", assets("static", "/static/"))
	mux.Handle("GET /dist/", assets("dist", "/dist/"))
	mux.Handle("GET /manifest.webmanifest", s.manifestFile())
	mux.Handle("GET /sw.js", embedFile("dist/sw.js", "text/javascript; charset=utf-8"))
	mux.Handle("GET /{$}", http.RedirectHandler("/app", http.StatusSeeOther))
	mux.Handle("GET /app", g.page(s.app))
	mux.Handle("GET /api/tree", g.page(s.apiTree))
	mux.Handle("GET /api/stream", g.stream(s.apiStream))
	mux.Handle("GET /api/logs", g.stream(s.apiLogs))
	mux.Handle("GET /api/host", g.page(s.apiHost))
	mux.Handle("GET /api/procs", g.page(s.apiProcs))
	mux.Handle("GET /api/history", g.page(s.apiHistory))
	mux.Handle("GET /api/history/top", g.page(s.apiHistoryTop))
	mux.Handle("GET /api/history/sessions", g.page(s.apiHistorySessions))
	mux.Handle("GET /api/history/net", g.page(s.apiHistoryNet))
	mux.Handle("GET /api/alerts", g.page(s.apiAlerts))
	mux.Handle("POST /api/alerts/{id}/ack", g.page(s.apiAlertAck))
	mux.Handle("GET /api/chat", g.page(s.apiChat))
	mux.Handle("GET /api/chat/stream", g.stream(s.apiChatStream))
	mux.Handle("GET /api/chat/image", g.page(s.apiChatImage))
	mux.Handle("GET /api/chat/call", g.page(s.apiChatCall))
	mux.Handle("GET /api/chat/task", g.page(s.apiChatTask))
	mux.Handle("GET /api/chat/agent", g.page(s.apiChatAgent))
	mux.Handle("GET /api/chat/file", g.page(s.apiChatFile))
	mux.Handle("GET /api/chat/file/download", g.page(s.apiChatDownload))
	mux.Handle("GET /api/sessions/archive", g.page(s.apiSessionsArchive))
	// The viewer: everything about a repository comes through the agent, and
	// the diff is cut and coloured here before it goes on the wire.
	mux.Handle("GET /api/repo/refs", g.page(s.apiRepoRefs))
	mux.Handle("GET /api/repo/changes", g.page(s.apiRepoChanges))
	mux.Handle("GET /api/repo/tree", g.page(s.apiRepoTree))
	mux.Handle("GET /api/repo/find", g.page(s.apiRepoFind))
	mux.Handle("GET /api/repo/file", g.page(s.apiRepoFile))
	mux.Handle("GET /api/repo/diff", g.page(s.apiRepoDiff))
	mux.Handle("GET /api/repo/commit", g.page(s.apiRepoCommit))

	mux.Handle("GET /api/briefs", g.page(s.apiBriefs))
	mux.Handle("GET /api/briefs/{id}", g.page(s.apiBrief))
	mux.Handle("PUT /api/briefs/{id}", g.page(s.apiBriefDraft))
	mux.Handle("POST /api/briefs/{id}/sent", g.page(s.apiBriefSent))
	mux.Handle("DELETE /api/briefs/{id}", g.page(s.apiBriefDrop))
	mux.Handle("GET /api/artifacts", g.page(s.apiPages))
	mux.Handle("GET /api/artifacts/{id}", g.page(s.apiPageCard))
	mux.Handle("GET /api/artifacts/{id}/page", g.page(s.apiPage))

	mux.Handle("GET /api/usage/summary", g.page(s.apiUsageSummary))
	mux.Handle("GET /api/usage/series", g.page(s.apiUsageSeries))
	mux.Handle("GET /api/usage/breakdown", g.page(s.apiUsageBreakdown))
	mux.Handle("GET /api/usage/models", g.page(s.apiUsageModels))
	mux.Handle("GET /api/usage/tools", g.page(s.apiUsageTools))
	mux.Handle("GET /api/usage/contours", g.page(s.apiUsageContours))
	mux.Handle("GET /api/usage/scan", g.page(s.apiUsageScan))
	mux.Handle("POST /api/usage/scan", g.page(s.apiUsageScanStart))

	mux.Handle("GET /api/profiles", g.page(s.apiProfiles))
	mux.Handle("POST /api/profiles", g.page(s.apiCreateProfile))
	mux.Handle("PATCH /api/profiles/{id}", g.page(s.apiUpdateProfile))
	mux.Handle("DELETE /api/profiles/{id}", g.page(s.apiDeleteProfile))
	mux.Handle("PUT /api/profiles/order", g.page(s.apiReorderProfiles))
	mux.Handle("POST /api/profiles/{id}/groups", g.page(s.apiCreateGroup))
	mux.Handle("PUT /api/profiles/{id}/groups/order", g.page(s.apiReorderGroups))
	mux.Handle("PATCH /api/groups/{id}", g.page(s.apiUpdateGroup))
	mux.Handle("DELETE /api/groups/{id}", g.page(s.apiDeleteGroup))
	mux.Handle("POST /api/groups/{id}/projects", g.page(s.apiCreateProject))
	mux.Handle("PUT /api/groups/{id}/projects/order", g.page(s.apiReorderProjects))
	mux.Handle("PATCH /api/projects/{id}", g.page(s.apiUpdateProject))
	mux.Handle("DELETE /api/projects/{id}", g.page(s.apiDeleteProject))
	mux.Handle("POST /api/disk/hidden", g.page(s.apiHideDir))
	mux.Handle("DELETE /api/disk/hidden", g.page(s.apiShowDir))
	mux.Handle("GET /api/probes", g.page(s.apiProbes))
	mux.Handle("GET /api/settings", g.page(s.apiSettings))
	mux.Handle("GET /api/degradations", g.page(s.apiDegradations))
	mux.Handle("GET /api/actions", g.page(s.apiActions))
	mux.Handle("POST /api/actions", g.page(s.apiRunAction))
	mux.Handle("GET /api/exec", g.page(s.apiExecStatus))
	mux.Handle("GET /api/session/permission", g.page(s.apiSessionPermission))
	mux.Handle("GET /api/session/window", g.page(s.apiSessionWindow))
	mux.Handle("GET /api/faults", g.page(s.apiFaults))

	mux.HandleFunc("POST /auth/passkey/login/begin", s.passkey.BeginLogin)
	mux.HandleFunc("POST /auth/passkey/login/finish", s.passkey.FinishLogin)
	mux.HandleFunc("POST /auth/token", s.tokens.Login)
	mux.HandleFunc("GET /auth/methods", auth.Methods(s.passkey, s.tokens, s.loginHome(g)))
	mux.HandleFunc("POST /auth/passkey/register/begin", s.passkey.BeginRegister)
	mux.HandleFunc("POST /auth/passkey/register/finish", s.passkey.FinishRegister)
	mux.Handle("GET /api/devices", g.page(s.passkey.ListDevices))
	mux.Handle("PATCH /api/devices/{id}", g.page(s.passkey.RenameDevice))
	mux.Handle("DELETE /api/devices/{id}", g.page(s.passkey.RevokeDevice))
	mux.Handle("POST /api/enroll/code", g.page(s.passkey.IssueCode))
	mux.Handle("GET /api/push/key", g.page(s.apiPushKey))
	mux.Handle("GET /api/push/subscription", g.page(s.passkey.Subscription))
	mux.Handle("POST /api/push/subscription", g.page(s.passkey.Subscribe))
	mux.Handle("DELETE /api/push/subscription", g.page(s.passkey.Unsubscribe))
	mux.Handle("POST /api/push/test", g.page(s.apiPushTest))

	if g.term {
		mux.Handle("GET /api/term", g.page(s.apiTermStatus))
		mux.Handle("GET /api/term/stream", g.stream(s.apiTermStream))
		mux.Handle("POST /api/term/input", g.page(s.apiTermInput))
		mux.Handle("POST /api/term/size", g.page(s.apiTermSize))
	}

	return mux
}
