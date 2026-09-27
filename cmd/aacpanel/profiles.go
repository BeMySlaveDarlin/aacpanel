package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/contours"
	"aacpanel/internal/host"
	"aacpanel/internal/launcher"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

const profileBodyMax = 64 << 10

func (s *Server) apiProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	s.writeProfiles(w, r, nil)
}

// apiProfilesSchema answers what the launch parameters of the map are: the
// screens are drawn from it, and it needs no database — the schema is built
// into the service.
func (s *Server) apiProfilesSchema(w http.ResponseWriter, r *http.Request) {
	traits := map[string]host.ModelTrait{}
	if s.host != nil {
		traits = s.host.ModelTraits()
	}
	writeJSON(w, map[string]any{
		"params":  schema.Params(),
		"retired": schema.RetiredKeys(),
		"layers":  []string{schema.LayerClaude, schema.LayerPanel, schema.LayerAccount, schema.LayerContour, schema.LayerProject},
		// What each model takes: the efforts a screen offers for it and
		// whether it has auto. Empty until a session on the stream says it.
		"traits": traits,
	})
}

func (s *Server) apiCreateProfile(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	var body store.ProfileEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	p, err := s.db.CreateProfile(r.Context(), body)
	if err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"profile": p})
}

func (s *Server) apiUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "profile")
	if !ok {
		return
	}
	var body store.ProfileEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if body.Name != nil {
		if err := s.keepPersonalName(r.Context(), id, *body.Name); err != nil {
			profilesError(w, err)
			return
		}
	}
	p, err := s.db.UpdateProfile(r.Context(), id, body)
	if err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"profile": p})
}

func (s *Server) apiDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "profile")
	if !ok {
		return
	}
	if err := s.db.DeleteProfile(r.Context(), id, cascadeAsked(r)); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) apiCreateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "profile")
	if !ok {
		return
	}
	var body store.GroupEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	g, err := s.db.CreateGroup(r.Context(), id, body)
	if err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"group": g})
}

func (s *Server) apiUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "group")
	if !ok {
		return
	}
	var body store.GroupEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	g, err := s.db.UpdateGroup(r.Context(), id, body)
	if err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"group": g})
}

func (s *Server) apiDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "group")
	if !ok {
		return
	}
	if err := s.db.DeleteGroup(r.Context(), id, cascadeAsked(r)); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) apiCreateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "group")
	if !ok {
		return
	}
	var body store.ProjectEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	p, err := s.db.CreateProject(r.Context(), id, body)
	if err != nil {
		profilesError(w, err)
		return
	}
	if err := s.prepareProjectDir(r.Context(), p); err != nil {
		projectDirError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"project": p})
}

func (s *Server) prepareProjectDir(ctx context.Context, p store.ProfileProject) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), execTimeout)
	defer cancel()

	err := s.makeProjectDir(ctx, p.Path)
	if err == nil {
		return nil
	}
	if _, drop := s.db.DeleteProject(ctx, p.ID); drop != nil {
		log.Printf("profile map: the directory for project %d was not created (%v), "+
			"and the entry was not rolled back either: %v", p.ID, err, drop)
		return fmt.Errorf("%w; and the map entry was not rolled back either (%v) — "+
			"delete project %d by hand, there is no directory behind it", err, drop, p.ID)
	}
	return fmt.Errorf("%w; the map entry was rolled back, nothing was added", err)
}

func (s *Server) makeProjectDir(ctx context.Context, path string) error {
	if s.exec == nil {
		return fmt.Errorf("%w: the directory %s has nobody to create it", action.ErrUnavailable, path)
	}
	resp, err := s.exec.Do(ctx, action.Request{
		ID:     requestID(0),
		Kind:   action.ProjectCreate,
		Target: path,
	})
	switch {
	case err != nil:
		return fmt.Errorf("the directory %s was not created: %w", path, err)
	case !resp.OK:
		return fmt.Errorf("the directory %s was not created: %s", path, resp.Error)
	}
	return nil
}

func projectDirError(w http.ResponseWriter, err error) {
	if errors.Is(err, action.ErrUnavailable) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *Server) apiUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "project")
	if !ok {
		return
	}
	var body store.ProjectEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	p, err := s.db.UpdateProject(r.Context(), id, body)
	if err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"project": p})
}

func (s *Server) apiDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "project")
	if !ok {
		return
	}
	entry, err := s.db.DeleteProject(r.Context(), id)
	if err != nil {
		profilesError(w, err)
		return
	}
	// The entry of the journal the deletion made: the screen offers to take
	// it back by it.
	s.writeProfiles(w, r, map[string]any{"undo": entry})
}

// apiProfilesJournal answers the latest changes of the map, the newest first.
func (s *Server) apiProfilesJournal(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.db.Journal(r.Context(), limit)
	if err != nil {
		profilesError(w, err)
		return
	}
	writeJSON(w, map[string]any{"journal": list})
}

// apiUndoJournal takes back the change a journal entry names: a deleted
// project comes back as it was.
func (s *Server) apiUndoJournal(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "the journal entry is not a number", http.StatusBadRequest)
		return
	}
	p, err := s.db.UndoDelete(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotUndoable) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, map[string]any{"project": p})
}

// mapActor names who changes the map, for its journal: the device of the
// request, as the actions journal names it.
func (s *Server) mapActor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(store.WithActor(r.Context(), s.deviceName(r))))
	}
}

func cascadeAsked(r *http.Request) bool {
	return r.URL.Query().Get("cascade") == "1"
}

type orderBody struct {
	IDs []int `json:"ids"`
}

func (s *Server) apiReorderProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	var body orderBody
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if err := s.db.ReorderProfiles(r.Context(), body.IDs); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) apiReorderGroups(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "profile")
	if !ok {
		return
	}
	var body orderBody
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if err := s.db.ReorderGroups(r.Context(), id, body.IDs); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) apiReorderProjects(w http.ResponseWriter, r *http.Request) {
	id, ok := s.profileID(w, r, "group")
	if !ok {
		return
	}
	var body orderBody
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if err := s.db.ReorderProjects(r.Context(), id, body.IDs); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

type dirBody struct {
	Path string `json:"path"`
}

func (s *Server) apiHideDir(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	var body dirBody
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if err := s.db.HideDir(r.Context(), body.Path); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) apiShowDir(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	var body dirBody
	if !decodeProfileBody(w, r, &body) {
		return
	}
	if err := s.db.ShowDir(r.Context(), body.Path); err != nil {
		profilesError(w, err)
		return
	}
	s.writeProfiles(w, r, nil)
}

func (s *Server) writeProfiles(w http.ResponseWriter, r *http.Request, extra map[string]any) {
	if r.Method != http.MethodGet {
		s.guardsChanged()
	}
	list, err := s.db.Profiles(r.Context())
	if err != nil {
		profilesError(w, err)
		return
	}
	s.fillMap(list)
	body := map[string]any{"profiles": list, "models": s.modelCatalog(), "disk": s.diskReport(r.Context(), list)}
	for k, v := range extra {
		body[k] = sameEntry(list, v)
	}
	writeJSON(w, body)
}

// fillMap puts on the map what the host says of each contour's account, the
// effective values of every entry and the command every project's next launch
// runs.
func (s *Server) fillMap(list []store.Profile) {
	states := s.contourStates()
	for i := range list {
		st := states.of(list[i])
		list[i].Auth, list[i].Hooks = st.Auth, st.Hooks
		list[i].Account, list[i].ContextGuard = st.Account, st.ContextGuard
	}
	store.FillEffective(list)
	for i := range list {
		for g := range list[i].Groups {
			for p := range list[i].Groups[g].Projects {
				fillLine(list[i], &list[i].Groups[g].Projects[p])
			}
		}
	}
}

// fillLine puts on a project the command its next launch runs, built by the
// launcher's own code and started by nobody, each word marked with the layer
// its parameter came from.
func fillLine(contour store.Profile, project *store.ProfileProject) {
	launch, err := store.EffectiveLaunch(contour.Launch, project.Launch)
	if err != nil {
		return
	}
	line := launcher.Preview(sessionNameOf(*project), launch)
	layers := map[string]string{}
	for _, v := range project.Effective {
		layers[v.Key] = v.Layer
	}
	for _, words := range [][]schema.Word{line.Words, line.Then} {
		for w := range words {
			words[w].Layer = layers[words[w].Key]
		}
	}
	project.Line = &line
}

// apiPreviewProject answers what a project would launch with if a draft of
// its edit were saved: its effective values, the command and what the launch
// would refuse. Nothing is written: the screen asks as the draft changes, so
// the command it shows is built by the launcher's own code and not guessed.
func (s *Server) apiPreviewProject(w http.ResponseWriter, r *http.Request) {
	if !s.profilesReady(w) {
		return
	}
	id, ok := s.profileID(w, r, "project")
	if !ok {
		return
	}
	var body store.ProjectEdit
	if !decodeProfileBody(w, r, &body) {
		return
	}
	list, err := s.db.Profiles(r.Context())
	if err != nil {
		profilesError(w, err)
		return
	}
	s.fillMap(list)
	contour, project := findProject(list, id)
	if project == nil {
		http.Error(w, fmt.Sprintf("there is no project %d", id), http.StatusNotFound)
		return
	}
	draft, problems, err := store.DraftLaunch(project.Launch, body.LaunchSet, body.LaunchUnset)
	if err != nil {
		profilesError(w, err)
		return
	}
	project.Launch = draft
	if body.Session != nil {
		project.Session = *body.Session
	}
	if body.Path != nil {
		project.Path = *body.Path
	}
	project.Effective = store.EffectiveOf(*contour, draft)
	fillLine(*contour, project)
	if problems == nil {
		problems = []schema.Problem{}
	}
	writeJSON(w, map[string]any{"effective": project.Effective, "line": project.Line, "problems": problems})
}

func findProject(list []store.Profile, id int) (*store.Profile, *store.ProfileProject) {
	for i := range list {
		for g := range list[i].Groups {
			for p := range list[i].Groups[g].Projects {
				if list[i].Groups[g].Projects[p].ID == id {
					return &list[i], &list[i].Groups[g].Projects[p]
				}
			}
		}
	}
	return nil, nil
}

// sameEntry returns the entry of the map an answer names beside it, as the
// map has it — with the account state and the effective values filled — so
// the two never disagree. An entry the map no longer holds goes as it is.
func sameEntry(list []store.Profile, v any) any {
	for _, p := range list {
		if want, ok := v.(store.Profile); ok && p.ID == want.ID {
			return p
		}
		for _, g := range p.Groups {
			if want, ok := v.(store.ProfileGroup); ok && g.ID == want.ID {
				return g
			}
			for _, project := range g.Projects {
				if want, ok := v.(store.ProfileProject); ok && project.ID == want.ID {
					return project
				}
			}
		}
	}
	return v
}

func (s *Server) keepPersonalName(ctx context.Context, id int, name string) error {
	if strings.TrimSpace(name) == contours.Personal {
		return nil
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.ID == id && p.Name == contours.Personal {
			return fmt.Errorf("profile %q is not renamed: this is a protocol name, and by it the panel, the "+
				"collector and the wizard find its config directory: %w",
				contours.Personal, store.ErrConflict)
		}
	}
	return nil
}

func (s *Server) contourPicks(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.db == nil {
		return nil, errors.New("the profile map is unavailable: the database does not answer")
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return nil, errors.New("the profile map is unavailable: the database does not answer")
	}
	byID := make(map[int]store.Profile, len(list))
	for _, p := range list {
		byID[p.ID] = p
	}
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("profile %q is not a map entry id", raw)
		}
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("there is no profile %d in the map: it was deleted, or the map changed", id)
		}
		dir := cleanDir(p.ConfigDir)
		if dir == "" {
			return nil, fmt.Errorf("profile %d has no config directory", id)
		}
		out = append(out, dir)
	}
	return out, nil
}

type contourMap struct {
	byDir  map[string]host.ContourState
	byName map[string]host.ContourState
}

func (c contourMap) of(p store.Profile) host.ContourState {
	if dir := cleanDir(p.ConfigDir); dir != "" && len(c.byDir) > 0 {
		return c.byDir[dir]
	}
	return c.byName[p.Name]
}

func (s *Server) contourStates() contourMap {
	out := contourMap{byDir: map[string]host.ContourState{}, byName: map[string]host.ContourState{}}
	if s.host == nil {
		return out
	}
	for _, c := range s.host.Contours() {
		if dir := cleanDir(c.ConfigDir); dir != "" {
			out.byDir[dir] = c
		}
		if c.Name != "" {
			out.byName[c.Name] = c
		}
	}
	return out
}

func cleanDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

func limitsByContour(payload []byte, list []store.Profile) []byte {
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}
	var head map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["limits"], &head); err != nil {
		return payload
	}
	byDir := make(map[string]int, len(list))
	for _, p := range list {
		if dir := cleanDir(p.ConfigDir); dir != "" {
			byDir[dir] = p.ID
		}
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(head["contours"], &rows); err == nil {
		for _, row := range rows {
			putContour(row, byDir)
		}
		if raw, err := json.Marshal(rows); err == nil {
			head["contours"] = raw
		}
	}
	putContour(head, byDir)
	raw, err := json.Marshal(head)
	if err != nil {
		return payload
	}
	snapshot["limits"] = raw
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}

func sessionsByContour(payload []byte, list []store.Profile) []byte {
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["sessions"], &rows); err != nil {
		return payload
	}
	byDir := make(map[string]int, len(list))
	for _, p := range list {
		if dir := cleanDir(p.ConfigDir); dir != "" {
			byDir[dir] = p.ID
		}
	}
	for _, row := range rows {
		putContour(row, byDir)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return payload
	}
	snapshot["sessions"] = raw
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}

func putContour(row map[string]json.RawMessage, byDir map[string]int) {
	raw, ok := row["configDir"]
	delete(row, "configDir")
	if !ok {
		return
	}
	var dir string
	if err := json.Unmarshal(raw, &dir); err != nil {
		return
	}
	if id := byDir[cleanDir(dir)]; id != 0 {
		row["contour"] = json.RawMessage(strconv.Itoa(id))
	}
}

func (s *Server) modelCatalog() host.Catalog {
	if s.host == nil {
		return host.Catalog{State: host.CatalogUnknown}
	}
	return s.host.ModelCatalog()
}

func (s *Server) profilesReady(w http.ResponseWriter) bool {
	if s.db == nil {
		http.Error(w, "the profile map is unavailable: the database is not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) profileID(w http.ResponseWriter, r *http.Request, what string) (int, bool) {
	if !s.profilesReady(w) {
		return 0, false
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "a "+what+" id is required", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func decodeProfileBody(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, profileBodyMax))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		http.Error(w, "the request was not parsed: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func profilesError(w http.ResponseWriter, err error) {
	var bad store.ErrBadRequest
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotEmpty):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, store.ErrUnavailable), errors.Is(err, store.ErrClosed):
		http.Error(w, "the profile map is unavailable: the database does not answer", http.StatusServiceUnavailable)
	default:
		log.Printf("profile map: %v", err)
		http.Error(w, "the profile map is unavailable", http.StatusInternalServerError)
	}
}
