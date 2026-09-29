package view

import (
	"strconv"
	"strings"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// asking is a block of the survey on the screen: its questions as the
// screen draws them, and the survey's own beside them, so an answer is read
// back by the place of the option rather than by what the screen shows.
type asking struct {
	block install.BlockID
	ui    *ui.Block
	specs []install.Question // parallel to ui.Questions
}

func newAsking(s *install.Survey, b install.BlockID, title string, qs []install.Question) *asking {
	a := &asking{block: b, specs: qs}
	uqs := make([]ui.Question, len(qs))
	for i, q := range qs {
		uqs[i] = toUI(q)
	}
	a.ui = ui.NewBlock(title, uqs...)
	if b != "" {
		a.ui.Refresh = func(*ui.Block) { a.refresh(s) }
	}
	return a
}

// toUI is a question of the survey as the screen draws it. The value of an
// option is its place: an empty value — no windows — is an answer, and the
// screen reads an empty value as none.
func toUI(q install.Question) ui.Question {
	u := ui.Question{ID: q.ID, Tab: q.Tab, Prompt: q.Prompt, Note: q.Note, Own: q.Own, Skip: q.Skip,
		Check: q.Check, Hidden: q.Moot}
	switch q.Form {
	case install.Many:
		u.Kind = ui.Multi
	case install.Line:
		u.Kind = ui.Text
	case install.Secret:
		u.Kind = ui.Secret
	}
	found := false
	for i, o := range q.Options {
		u.Options = append(u.Options, ui.Option{Label: o.Name(), Detail: o.Detail, Group: o.Group,
			Locked: o.Locked, On: o.On, Value: strconv.Itoa(i)})
		if q.Form == install.One && !found && o.Value == q.Default {
			u.Default, found = i, true
		}
	}
	return u
}

// value is the answer to the question at i as the survey takes it.
func (a *asking) value(i int) string {
	q, ans := a.specs[i], a.ui.Answers[i]
	uq := a.ui.Questions[i]
	switch q.Form {
	case install.One:
		if ans.Choice < len(q.Options) {
			return q.Options[ans.Choice].Value
		}
		return ans.Text
	case install.Many:
		var out []string
		for j, on := range ans.Picks {
			if !on {
				continue
			}
			if j < len(q.Options) {
				out = append(out, q.Options[j].Value)
			} else if j < len(uq.Options) {
				out = append(out, uq.Options[j].Label)
			}
		}
		return strings.Join(out, ",")
	}
	return ans.Text
}

// refresh hands the answers so far to the survey and takes back what they
// change in the block: a question made moot, a prompt that names an
// earlier answer, a suggestion that follows one.
func (a *asking) refresh(s *install.Survey) {
	a.give(s)
	now := map[string]install.Question{}
	for _, q := range s.Questions(a.block) {
		now[q.ID] = q
	}
	for i := range a.specs {
		q, ok := now[a.specs[i].ID]
		uq := &a.ui.Questions[i]
		if !ok {
			uq.Hidden = true
			continue
		}
		uq.Hidden = q.Moot
		uq.Prompt, uq.Check = q.Prompt, q.Check
		if len(q.Options) != len(a.specs[i].Options) && !a.ui.Answers[i].Given {
			a.ui.Replace(i, toUI(q))
			a.specs[i] = q
			continue
		}
		if q.Form == install.One && len(q.Options) == len(a.specs[i].Options) {
			for j, o := range q.Options {
				uq.Options[j].Label, uq.Options[j].Detail = o.Name(), o.Detail
			}
			a.specs[i].Options = q.Options
			a.specs[i].Default = q.Default
		}
	}
}

// give sets the answers of the questions the block shows.
func (a *asking) give(s *install.Survey) {
	s.Clear(a.block)
	for i, q := range a.specs {
		if a.ui.Questions[i].Hidden {
			continue
		}
		v := a.value(i)
		s.Set(q, v, s.Source(q, v))
	}
}
