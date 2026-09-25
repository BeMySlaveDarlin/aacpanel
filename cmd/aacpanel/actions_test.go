package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

type fakeExec struct {
	resp action.Response
	got  chan action.Request
}

func startFakeExec(t *testing.T, resp action.Response) (*action.Client, *fakeExec) {
	t.Helper()

	sock := socketPath(t, "exec.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	f := &fakeExec{resp: resp, got: make(chan action.Request, 64)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var req action.Request
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				f.got <- req
				out := f.resp
				out.ID = req.ID
				json.NewEncoder(conn).Encode(out)
			}()
		}
	}()

	return action.NewClient(sock, 5*time.Second), f
}

func post(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(body))
	srv.apiRunAction(w, r)
	return w
}

func TestRunActionWithoutExecutor(t *testing.T) {
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}}
	w := post(t, srv, `{"kind":"container.stop","target":"app"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("without an executor the status is %d, expected 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "executor is not configured") {
		t.Errorf("the refusal %q does not name what is missing — one will go looking for the breakage in the panel",
			strings.TrimSpace(w.Body.String()))
	}
}

func TestRunActionValidates(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	bad := map[string]string{
		"an unknown action":          `{"kind":"container.remove","target":"app"}`,
		"an action without a target": `{"kind":"container.stop","target":""}`,
		"a target with a space":      `{"kind":"container.stop","target":"app; rm -rf /"}`,
		"a target with a newline":    "{\"kind\":\"container.stop\",\"target\":\"app\\nrm\"}",
		"an escape out of the path":  `{"kind":"session.open","target":"../../etc"}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			if w := post(t, srv, body); w.Code != http.StatusBadRequest {
				t.Errorf("status %d, expected 400", w.Code)
			}
		})
	}

	select {
	case req := <-fake.got:
		t.Errorf("a malformed request reached the executor: %+v", req)
	default:
	}
}

func TestRunActionPassesStructureNotString(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "app stopped", DurationMs: 12})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"container.stop","target":"app"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}

	if got.Kind != action.ContainerStop || got.Target != "app" {
		t.Errorf("%+v went to the executor", got)
	}
	if got.ID == "" {
		t.Error("a request without an id — idempotency does not work")
	}

	var body struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Logged bool   `json:"logged"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if !body.OK || body.Detail != "app stopped" {
		t.Errorf("the response to the front: %s", w.Body.String())
	}
	if body.Logged {
		t.Error("without a database the handler reported a write to the action log")
	}
}

func TestRunActionLogsDetailPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	client, _ := startFakeExec(t, action.Response{
		OK:         true,
		Detail:     "session aacpanel-2 started",
		DurationMs: 340,
	})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	w := post(t, srv, `{"kind":"session.open","target":"/srv/proj/Beta/service/aacpanel"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the action log")
	}
	if list[0].Detail != "session aacpanel-2 started" {
		t.Fatalf("the action log lost the detail from the executor: %+v", list[0])
	}
}

func TestRunActionReportsFailure(t *testing.T) {
	client, _ := startFakeExec(t, action.Response{OK: false, Error: "docker knows no container"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"container.stop","target":"app"}`)
	if w.Code == http.StatusOK {
		t.Errorf("a refusal came with status 200: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "knows no container") {
		t.Errorf("the reason for the refusal was lost: %s", w.Body.String())
	}
}

func TestRunActionWhenExecutorIsDown(t *testing.T) {
	dead := action.NewClient(socketPath(t, "missing.sock"), time.Second)
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: dead}

	w := post(t, srv, `{"kind":"container.stop","target":"app"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, expected 503 for an unreachable executor", w.Code)
	}
}

func TestRequestIDFollowsJournal(t *testing.T) {
	if a, b := requestID(17), requestID(17); a != b {
		t.Errorf("action log id 17 gave different request ids: %q and %q", a, b)
	}
	if a, b := requestID(0), requestID(0); a == b {
		t.Error("without the action log the ids matched — a repeat will be counted as the same action")
	}
	if err := (action.Request{ID: requestID(0), Kind: action.ContainerStop, Target: "app"}).Validate(); err != nil {
		t.Errorf("the generated id does not pass validation: %v", err)
	}
}

func TestDeviceRef(t *testing.T) {
	if deviceRef(0) != nil {
		t.Error("a zero device id did not turn into NULL")
	}
	if got := deviceRef(5); got == nil || *got != 5 {
		t.Errorf("the device id was lost: %v", got)
	}
}

func TestSessionSendKeepsTextOutOfJournalPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "delivered to aacpanel (idle), 21 chars"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	const secret = "check the mail, the password is there too"
	w := post(t, srv, `{"kind":"session.send","target":"aacpanel","params":{"text":"`+secret+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	req := <-exec.got
	if req.Text != secret {
		t.Errorf("text %q went to the executor, while the person typed %q", req.Text, secret)
	}
	if req.Target != "aacpanel" {
		t.Errorf("target %q is the wrong session", req.Target)
	}

	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the action log")
	}
	body, err := json.Marshal(list[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("the text of the reply settled in the action log: %s", body)
	}
	if !strings.Contains(string(body), "chars") {
		t.Errorf("the action log did not keep the length of the reply: %s", body)
	}
}

func TestRunActionCarriesAnswerToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "answer sent to aacpanel: Beta"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.answer","target":"aacpanel",`+
		`"params":{"ask":"toolu_42","picks":[[2],[1,3]]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("the answer to the question was rejected: status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Answer == nil {
		t.Fatal("a request without picks went to the executor — there is nothing to answer with")
	}
	if got.Answer.AskID != "toolu_42" {
		t.Errorf("the question id did not arrive: %q", got.Answer.AskID)
	}
	want := [][]int{{2}, {1, 3}}
	if len(got.Answer.Picks) != len(want) {
		t.Fatalf("%d questions in the answer, expected %d: %v", len(got.Answer.Picks), len(want), got.Answer.Picks)
	}
	for i := range want {
		if len(got.Answer.Picks[i]) != len(want[i]) {
			t.Fatalf("the picks for question %d arrived as %v, expected %v", i+1, got.Answer.Picks[i], want[i])
		}
		for k := range want[i] {
			if got.Answer.Picks[i][k] != want[i][k] {
				t.Errorf("question %d: option %d arrived as %d, expected %d",
					i+1, k+1, got.Answer.Picks[i][k], want[i][k])
			}
		}
	}
}

func TestRunActionRefusesAnswerWithoutPicks(t *testing.T) {
	client, _ := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.answer","target":"aacpanel","params":{"ask":"toolu_42","picks":[]}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an empty pick was accepted: status %d, body %s", w.Code, w.Body.String())
	}
}

func TestRunActionCarriesFileToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "the file lies in /home/u/x"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	raw := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0x1a, 0x0a}
	body := `{"kind":"session.file","target":"aacpanel","params":{"text":"look",` +
		`"files":[{"name":"shot.png","data":"` + base64.StdEncoding.EncodeToString(raw) + `"}]}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("the file was rejected: status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if len(got.Files) != 1 {
		t.Fatalf("%d attachments went to the executor, expected one — there is nothing to send", len(got.Files))
	}
	if got.Files[0].Name != "shot.png" {
		t.Errorf("the file name did not arrive: %q", got.Files[0].Name)
	}
	if string(got.Files[0].Data) != string(raw) {
		t.Errorf("the content arrived damaged: %v", got.Files[0].Data)
	}
	if got.Text != "look" {
		t.Errorf("the caption for the file did not arrive: %q", got.Text)
	}
}

func TestRunActionCarriesEveryFileToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "3 files"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	want := []struct {
		name string
		data []byte
	}{
		{"first.png", []byte{0x89, 'P', 'N', 'G', 0x00}},
		{"second.log", []byte("two lines\nsecond")},
		{"third.bin", []byte{0xff, 0x00, 0x1a}},
	}
	items := make([]string, 0, len(want))
	for _, f := range want {
		items = append(items, `{"name":"`+f.name+`","data":"`+base64.StdEncoding.EncodeToString(f.data)+`"}`)
	}
	body := `{"kind":"session.file","target":"aacpanel","params":{"text":"look at three at once",` +
		`"files":[` + strings.Join(items, ",") + `]}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("the batch was rejected: status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if len(got.Files) != len(want) {
		t.Fatalf("%d attachments out of %d went to the executor", len(got.Files), len(want))
	}
	for i, f := range want {
		if got.Files[i].Name != f.name {
			t.Errorf("attachment %d arrived under the name %q, expected %q", i+1, got.Files[i].Name, f.name)
		}
		if string(got.Files[i].Data) != string(f.data) {
			t.Errorf("the content of %s arrived damaged: %v", f.name, got.Files[i].Data)
		}
	}
	if got.Text != "look at three at once" {
		t.Errorf("the caption for the batch did not arrive: %q", got.Text)
	}
}

func TestRunActionSendsBiggestPack(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	const parts = 4
	items := make([]string, 0, parts)
	for i := range parts {
		raw := make([]byte, action.FilesBytesMax/parts)
		for j := range raw {
			raw[j] = byte(i + j)
		}
		items = append(items, `{"name":"part`+strconv.Itoa(i)+`.bin","data":"`+
			base64.StdEncoding.EncodeToString(raw)+`"}`)
	}
	body := `{"kind":"session.file","target":"aacpanel","params":{"files":[` + strings.Join(items, ",") + `]}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("a batch at the weight ceiling was rejected: status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if len(got.Files) != parts {
			t.Fatalf("%d attachments out of %d reached the executor", len(got.Files), parts)
		}
		total := 0
		for _, f := range got.Files {
			total += len(f.Data)
		}
		if total != action.FilesBytesMax {
			t.Errorf("%d bytes out of %d arrived", total, action.FilesBytesMax)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the executor did not get the request")
	}
}

func TestRunActionSendsBiggestFile(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	raw := make([]byte, action.FileMax)
	for i := range raw {
		raw[i] = byte(i)
	}
	body := `{"kind":"session.file","target":"aacpanel","params":{"files":[{"name":"big.bin","data":"` +
		base64.StdEncoding.EncodeToString(raw) + `"}]}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("a file at the size ceiling was rejected: status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if len(got.Files) != 1 || len(got.Files[0].Data) != len(raw) {
			t.Fatalf("%d attachments reached the executor instead of one", len(got.Files))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the executor did not get the request")
	}
}

func TestRunActionKeepsOtherRequestsShort(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	long := strings.Repeat("a", actionBodyMax*2)
	w := post(t, srv, `{"kind":"container.stop","target":"app","params":{"note":"`+long+`"}}`)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a long request without a file was accepted: status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case req := <-fake.got:
		t.Errorf("a bloated request reached the executor: %+v", req)
	default:
	}
}

func TestRunActionCarriesCommandToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "/model opus typed in aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.command","target":"aacpanel","params":{"command":"model","arg":"opus"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("the command was rejected: status %d, body %s", w.Code, w.Body.String())
	}
	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Command == nil {
		t.Fatal("a request without a command went to the executor")
	}
	if got.Command.Name != "model" || got.Command.Arg != "opus" {
		t.Errorf("the command arrived as %+v, expected model/opus", got.Command)
	}
}

func TestRunActionRefusesCommandOutsideList(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.command","target":"aacpanel","params":{"command":"permissions"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a command outside the list was accepted: status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case req := <-fake.got:
		t.Errorf("a command outside the list reached the executor: %+v", req)
	default:
	}
}

func TestSessionFileKeepsBytesOutOfJournalPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	client, _ := startFakeExec(t, action.Response{OK: true, Detail: "the file lies in /home/u/x"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	secrets := []string{strings.Repeat("caption", 20), strings.Repeat("second secret", 20)}
	body := `{"kind":"session.file","target":"aacpanel","params":{"files":[` +
		`{"name":"shot.png","data":"` + base64.StdEncoding.EncodeToString([]byte(secrets[0])) + `"},` +
		`{"name":"log.txt","data":"` + base64.StdEncoding.EncodeToString([]byte(secrets[1])) + `"}]}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the action log")
	}
	raw, err := json.Marshal(list[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte(secret))) {
			t.Errorf("the content of the file settled in the action log: %s", raw)
		}
	}
	for _, name := range []string{"shot.png", "log.txt"} {
		if !strings.Contains(string(raw), name) {
			t.Errorf("the action log did not keep the file name %s: %s", name, raw)
		}
	}
	if !strings.Contains(string(raw), "bytes") {
		t.Errorf("the action log did not keep the file size: %s", raw)
	}
}

func TestRunActionCarriesOwnWordsToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "answer sent to aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	const own = "none of the options fit, let us take a third way"
	w := post(t, srv, `{"kind":"session.answer","target":"aacpanel",`+
		`"params":{"ask":"toolu_42","picks":[[],[1]],"texts":["`+own+`",""]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("a free-form answer was rejected: status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Answer == nil {
		t.Fatal("a request without an answer went to the executor")
	}
	if len(got.Answer.Texts) != 2 {
		t.Fatalf("%d free-form answers arrived, expected 2: %q", len(got.Answer.Texts), got.Answer.Texts)
	}
	if got.Answer.Texts[0] != own {
		t.Errorf("the free-form answer arrived as %q, and the person wrote %q", got.Answer.Texts[0], own)
	}
	if got.Answer.Texts[1] != "" {
		t.Errorf("words the person never wrote went out for the second question: %q", got.Answer.Texts[1])
	}
}

func TestRunActionDismissCarriesQuestion(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "the question was dismissed in aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.dismiss","target":"aacpanel","params":{"ask":"toolu_42"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("dismissing the question was rejected: status %d, body %s", w.Code, w.Body.String())
	}

	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Answer == nil || got.Answer.AskID != "toolu_42" {
		t.Fatalf("a dismissal without a question went to the executor: %+v", got.Answer)
	}
	if len(got.Answer.Picks) > 0 || len(got.Answer.Texts) > 0 {
		t.Errorf("the dismissal went out with an answer: %+v", got.Answer)
	}

	if w := post(t, srv, `{"kind":"session.dismiss","target":"aacpanel","params":{}}`); w.Code != http.StatusBadRequest {
		t.Errorf("a dismissal without a question was accepted: status %d, body %s", w.Code, w.Body.String())
	}
}

func TestAnswerTextsFitActionBody(t *testing.T) {
	texts := make([]string, 8)
	for i := range texts {
		// a two-byte rune: the answer is capped in runes, and the request body in bytes
		texts[i] = strings.Repeat("é", action.AskTextMax)
	}
	body, err := json.Marshal(map[string]any{
		"kind":   string(action.SessionAnswer),
		"target": "aacpanel",
		"params": map[string]any{"ask": "toolu_42", "picks": make([][]int, 8), "texts": texts},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > actionBodyMax {
		t.Fatalf("a free-form answer at the ceiling weighs %d bytes against a body ceiling of %d: "+
			"the panel gets 413 for what the protocol considers valid", len(body), actionBodyMax)
	}
}

func TestSessionAnswerKeepsOwnWordsOutOfJournalPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "answer sent to aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, db: db}

	const secret = "do not touch prod, the password is in the mail"
	w := post(t, srv, `{"kind":"session.answer","target":"aacpanel",`+
		`"params":{"ask":"toolu_42","picks":[[]],"texts":["`+secret+`"]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	req := <-exec.got
	if req.Answer == nil || len(req.Answer.Texts) != 1 || req.Answer.Texts[0] != secret {
		t.Fatalf("what went to the executor is not what the person wrote: %+v", req.Answer)
	}

	list, err := db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the action log")
	}
	body, err := json.Marshal(list[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("the free-form answer settled in the action log: %s", body)
	}
	if !strings.Contains(string(body), "chars") {
		t.Errorf("the action log did not keep the length of the free-form answer: %s", body)
	}
}

func TestPermissionHandlerKeepsThreeAnswersApart(t *testing.T) {
	dialog := &action.Permission{
		Tool:        "Bash command",
		Action:      []string{"touch probe.txt"},
		Options:     []action.PermOption{{N: 1, Text: "Yes"}, {N: 2, Text: "No"}},
		Fingerprint: "abc123",
	}

	for _, c := range []struct {
		name  string
		resp  action.Response
		state string
		check func(t *testing.T, body map[string]any)
	}{
		{
			name:  "a dialog is waiting",
			resp:  action.Response{OK: true, Permission: dialog},
			state: "ok",
			check: func(t *testing.T, body map[string]any) {
				perm, ok := body["permission"].(map[string]any)
				if !ok {
					t.Fatalf("the response has no parsed dialog: %v", body)
				}
				if perm["tool"] != "Bash command" {
					t.Errorf("the tool was lost on the way: %v", perm["tool"])
				}
				if perm["fingerprint"] != "abc123" {
					t.Errorf("the fingerprint did not arrive — there will be nothing to press with: %v", perm["fingerprint"])
				}
				if opts, _ := perm["options"].([]any); len(opts) != 2 {
					t.Errorf("%d options arrived, expected 2", len(opts))
				}
			},
		},
		{
			name:  "there is no dialog",
			resp:  action.Response{OK: true},
			state: "none",
		},
		{
			name:  "asking did not work out",
			resp:  action.Response{OK: false, Error: "the konsole terminal is silent"},
			state: "unknown",
			check: func(t *testing.T, body map[string]any) {
				if reason, _ := body["reason"].(string); !strings.Contains(reason, "terminal is silent") {
					t.Errorf("the reason was lost: %q", reason)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, _ := startFakeExec(t, c.resp)
			srv := &Server{exec: client, hostName: "STAND-01"}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/session/permission?name=aacpanel", nil)
			srv.apiSessionPermission(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("the response is not json: %s", w.Body.String())
			}
			if body["state"] != c.state {
				t.Fatalf("state %v, expected %q", body["state"], c.state)
			}
			if c.check != nil {
				c.check(t, body)
			}
		})
	}

	client, _ := startFakeExec(t, action.Response{OK: true})
	srv := &Server{exec: client, hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiSessionPermission(w, httptest.NewRequest(http.MethodGet, "/api/session/permission", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a request without a session name gave %d, expected 400", w.Code)
	}
}

func TestSessionWindowStatesStayApart(t *testing.T) {
	for _, c := range []struct {
		name   string
		resp   action.Response
		state  string
		reason string
	}{
		{
			name:  "the window is open",
			resp:  action.Response{OK: true, Window: &action.Window{Open: true}},
			state: "open",
		},
		{
			name:  "there is no window",
			resp:  action.Response{OK: true, Window: &action.Window{}},
			state: "none",
		},
		{
			name:   "asking did not work out",
			resp:   action.Response{OK: false, Error: "session shop does not live in tmux"},
			state:  "unknown",
			reason: "does not live in tmux",
		},
		{
			name:   "the executor does not know the question",
			resp:   action.Response{OK: true},
			state:  "unknown",
			reason: "did not answer",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, _ := startFakeExec(t, c.resp)
			srv := &Server{exec: client, hostName: "STAND-01"}

			w := httptest.NewRecorder()
			srv.apiSessionWindow(w, httptest.NewRequest(http.MethodGet, "/api/session/window?name=aacpanel", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("the response is not json: %s", w.Body.String())
			}
			if body["state"] != c.state {
				t.Fatalf("state %v, expected %q", body["state"], c.state)
			}
			if c.reason != "" {
				if reason, _ := body["reason"].(string); !strings.Contains(reason, c.reason) {
					t.Errorf("the reason %q does not contain %q", reason, c.reason)
				}
			}
		})
	}

	client, _ := startFakeExec(t, action.Response{OK: true})
	srv := &Server{exec: client, hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiSessionWindow(w, httptest.NewRequest(http.MethodGet, "/api/session/window", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a request without a session name gave %d, expected 400", w.Code)
	}
}

func TestRunActionCarriesNotesToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "answer sent to aacpanel"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	const note = "but keep the search in it"
	w := post(t, srv, `{"kind":"session.answer","target":"aacpanel",`+
		`"params":{"ask":"toolu_42","picks":[[2],[1]],"notes":["`+note+`",""]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("an answer with a note was rejected: status %d, body %s", w.Code, w.Body.String())
	}
	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Answer == nil || len(got.Answer.Notes) != 2 || got.Answer.Notes[0] != note || got.Answer.Notes[1] != "" {
		t.Fatalf("the notes reached the executor as %+v", got.Answer)
	}
}

func TestRunActionCarriesTheMessageIDToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	const id = "11111111-2222-4333-8444-555555555555"
	for _, body := range []string{
		`{"kind":"session.send","target":"aacpanel","params":{"text":"hi","messageId":"` + id + `"}}`,
		`{"kind":"session.unqueue","target":"aacpanel","params":{"messageId":"` + id + `"}}`,
	} {
		if w := post(t, srv, body); w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		select {
		case got := <-fake.got:
			if got.MessageID != id {
				t.Errorf("%s reached the executor without its message id: %+v", got.Kind, got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
	}
	if w := post(t, srv, `{"kind":"session.unqueue","target":"aacpanel","params":{}}`); w.Code != http.StatusBadRequest {
		t.Errorf("taking back no message is accepted: %d", w.Code)
	}
}

// The picker asks once and gets both lists: what the session's claude names,
// and the catalogue of the account for the models it does not.
func TestSessionModelsCarryTheSessionAndTheCatalogue(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Models: &action.Models{
		Transport: "stream", Mode: "auto", Effort: "xhigh",
		List: []action.Model{{Value: "sonnet", Name: "Sonnet", Efforts: []string{"low", "high"}}}}})
	srv := &Server{exec: client, hostName: "STAND-01"}

	w := httptest.NewRecorder()
	srv.apiSessionModels(w, httptest.NewRequest(http.MethodGet, "/api/session/models?name=aacpanel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var body struct {
		State   string         `json:"state"`
		Session action.Models  `json:"session"`
		Catalog map[string]any `json:"catalog"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not json: %s", w.Body.String())
	}
	if body.State != "ok" || body.Session.Mode != "auto" || len(body.Session.List) != 1 ||
		body.Session.List[0].Value != "sonnet" || body.Catalog["state"] == nil {
		t.Errorf("the answer is %s", w.Body.String())
	}
	if got := <-fake.got; got.Ask != action.AskModels || got.Target != "aacpanel" {
		t.Errorf("the executor was asked %+v", got)
	}

	failing, _ := startFakeExec(t, action.Response{OK: false, Error: "no live session named aacpanel"})
	w = httptest.NewRecorder()
	(&Server{exec: failing}).apiSessionModels(w, httptest.NewRequest(http.MethodGet, "/api/session/models?name=aacpanel", nil))
	if !strings.Contains(w.Body.String(), `"state":"unknown"`) || !strings.Contains(w.Body.String(), "no live session") ||
		!strings.Contains(w.Body.String(), `"catalog"`) {
		t.Errorf("a refusal of the executor reads %s — the catalogue must still come", w.Body.String())
	}

	w = httptest.NewRecorder()
	srv.apiSessionModels(w, httptest.NewRequest(http.MethodGet, "/api/session/models", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a request without a session name gave %d, expected 400", w.Code)
	}
}

func TestRunActionCarriesTheModeToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	if w := post(t, srv, `{"kind":"session.set","target":"aacpanel","params":{"mode":"plan"}}`); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionSet || got.Setting == nil || got.Setting.Mode != "plan" || got.Setting.Model != "" {
			t.Errorf("the executor got %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if w := post(t, srv, `{"kind":"session.set","target":"aacpanel","params":{"mode":"bypassPermissions"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("a mode outside the four passed with %d", w.Code)
	}
	if w := post(t, srv, `{"kind":"session.set","target":"aacpanel","params":{"model":"sonnet","mode":"auto"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("two settings at once passed with %d", w.Code)
	}
}

// The list of MCP servers is the executor's answer passed on, and an empty
// list is a list: the screen reads it without guarding against its absence.
func TestSessionMcpPassesTheServersOn(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Mcp: &action.Mcp{Transport: "stream",
		Servers: []action.McpServer{{Name: "docker", Status: "connected", Tools: []string{"ps"}}}}})
	srv := &Server{exec: client, hostName: "STAND-01"}

	w := httptest.NewRecorder()
	srv.apiSessionMcp(w, httptest.NewRequest(http.MethodGet, "/api/session/mcp?name=aacpanel", nil))
	var body struct {
		State     string             `json:"state"`
		Transport string             `json:"transport"`
		Servers   []action.McpServer `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not json: %s", w.Body.String())
	}
	if body.State != "ok" || body.Transport != "stream" || len(body.Servers) != 1 || body.Servers[0].Name != "docker" {
		t.Errorf("the answer is %s", w.Body.String())
	}
	if got := <-fake.got; got.Ask != action.AskMcp || got.Target != "aacpanel" {
		t.Errorf("the executor was asked %+v", got)
	}

	empty, _ := startFakeExec(t, action.Response{OK: true, Mcp: &action.Mcp{Transport: "console"}})
	w = httptest.NewRecorder()
	(&Server{exec: empty}).apiSessionMcp(w, httptest.NewRequest(http.MethodGet, "/api/session/mcp?name=aacpanel", nil))
	if !strings.Contains(w.Body.String(), `"servers":[]`) {
		t.Errorf("a session with no list reads %s — the screen gets no list to read", w.Body.String())
	}

	failing, _ := startFakeExec(t, action.Response{OK: false, Error: "no live session named aacpanel"})
	w = httptest.NewRecorder()
	(&Server{exec: failing}).apiSessionMcp(w, httptest.NewRequest(http.MethodGet, "/api/session/mcp?name=aacpanel", nil))
	if !strings.Contains(w.Body.String(), `"state":"unknown"`) || !strings.Contains(w.Body.String(), "no live session") {
		t.Errorf("a refusal of the executor reads %s", w.Body.String())
	}
}

// A screen of settings is the executor's answer passed on under the name of
// its part, an empty list is a list, and a screen the panel does not know is
// refused before the executor is asked.
func TestSessionSetupPassesThePartOn(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Setup: &action.Setup{Transport: "stream",
		Skills: []action.Skill{{Name: "pdf", Source: "claude.ai sync", Tokens: 150, State: "on"}}}})
	srv := &Server{exec: client, hostName: "STAND-01"}

	w := httptest.NewRecorder()
	srv.apiSessionSetup(w, httptest.NewRequest(http.MethodGet, "/api/session/setup?name=aacpanel&part=skills", nil))
	var body struct {
		State     string         `json:"state"`
		Transport string         `json:"transport"`
		Skills    []action.Skill `json:"skills"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not json: %s", w.Body.String())
	}
	if body.State != "ok" || body.Transport != "stream" || len(body.Skills) != 1 || body.Skills[0].Name != "pdf" {
		t.Errorf("the answer is %s", w.Body.String())
	}
	if got := <-fake.got; got.Ask != action.AskSetup || got.Target != "aacpanel" || got.Part != action.SetupSkills {
		t.Errorf("the executor was asked %+v", got)
	}

	empty, _ := startFakeExec(t, action.Response{OK: true, Setup: &action.Setup{Transport: "stream"}})
	for part, want := range map[string]string{"agents": `"agents":[]`, "config": `"config":[]`,
		"hooks": `"hooks":{"events":[],"hooks":[]}`, "memory": `"files":[]`} {
		w = httptest.NewRecorder()
		(&Server{exec: empty}).apiSessionSetup(w, httptest.NewRequest(http.MethodGet,
			"/api/session/setup?name=aacpanel&part="+part, nil))
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("an empty %s reads %s — the screen gets nothing to read", part, w.Body.String())
		}
	}

	console, _ := startFakeExec(t, action.Response{OK: true, Setup: &action.Setup{Transport: "console"}})
	w = httptest.NewRecorder()
	(&Server{exec: console}).apiSessionSetup(w, httptest.NewRequest(http.MethodGet, "/api/session/setup?name=aacpanel&part=hooks", nil))
	if !strings.Contains(w.Body.String(), `"transport":"console"`) || strings.Contains(w.Body.String(), `"hooks"`) {
		t.Errorf("a terminal reads %s", w.Body.String())
	}

	asked, seen := startFakeExec(t, action.Response{OK: true})
	w = httptest.NewRecorder()
	(&Server{exec: asked}).apiSessionSetup(w, httptest.NewRequest(http.MethodGet, "/api/session/setup?name=aacpanel&part=permissions", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a screen the panel does not show answered %d", w.Code)
	}
	select {
	case got := <-seen.got:
		t.Errorf("the executor was asked %+v for a screen the panel does not show", got)
	default:
	}
}

func TestRunActionCarriesTheMcpChangeToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	body := `{"kind":"session.mcp","target":"aacpanel","params":{"server":"claude.ai Gmail","do":"disable"}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionMcp || got.Mcp == nil || got.Mcp.Server != "claude.ai Gmail" || got.Mcp.Do != "disable" {
			t.Errorf("the executor got %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if w := post(t, srv, `{"kind":"session.mcp","target":"aacpanel","params":{"server":"docker","do":"authenticate"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("an action outside the three passed with %d", w.Code)
	}
}

func TestRunActionCarriesTheNewNameToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	if w := post(t, srv, `{"kind":"session.rename","target":"person","params":{"name":"person-pilot"}}`); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionRename || got.Target != "person" || got.Rename != "person-pilot" {
			t.Errorf("the executor got %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if w := post(t, srv, `{"kind":"session.rename","target":"person","params":{"name":"../etc"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("a name with a path in it passed with %d", w.Code)
	}
}

func TestRunActionCarriesTheRemoteSwitchToExecutor(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	for _, on := range []bool{true, false} {
		body := fmt.Sprintf(`{"kind":"session.remote","target":"person","params":{"on":%v}}`, on)
		if w := post(t, srv, body); w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		select {
		case got := <-fake.got:
			if got.Kind != action.SessionRemote || got.Target != "person" || got.Remote == nil || *got.Remote != on {
				t.Errorf("the executor got %+v", got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
	}
	if w := post(t, srv, `{"kind":"session.remote","target":"person","params":{"on":"yes"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("a switch that is neither on nor off passed with %d", w.Code)
	}
}

// What a session says about itself is the executor's answer passed on.
func TestSessionStatusPassesTheAnswerOn(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Status: &action.Status{Transport: "stream",
		Version: "2.1.282", Account: &action.Account{Email: "a@b.c", Plan: "max"}}})
	srv := &Server{exec: client, hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiSessionStatus(w, httptest.NewRequest(http.MethodGet, "/api/session/status?name=aacpanel", nil))
	var body struct {
		State   string          `json:"state"`
		Version string          `json:"version"`
		Account *action.Account `json:"account"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not json: %s", w.Body.String())
	}
	if body.State != "ok" || body.Version != "2.1.282" || body.Account == nil || body.Account.Plan != "max" {
		t.Errorf("the answer is %s", w.Body.String())
	}
	if got := <-fake.got; got.Ask != action.AskStatus || got.Target != "aacpanel" {
		t.Errorf("the executor was asked %+v", got)
	}
	w = httptest.NewRecorder()
	srv.apiSessionStatus(w, httptest.NewRequest(http.MethodGet, "/api/session/status", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a request without a session name gave %d, expected 400", w.Code)
	}
}
