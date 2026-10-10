// Package contract holds what the panel reads and sends of the app-server
// protocol of codex against the JSON Schema codex writes of it.
//
// The daemon of a codex home updates itself, and its protocol moves between
// releases without a word to its clients: a field the panel decodes may go, a
// status may change its name, a method may take a new required param. The
// panel meets such a release blind — a decode that leaves a zero, a call the
// daemon refuses — unless the protocol is checked first. codex writes the
// schema of its own protocol without a session and without the network
// (`codex app-server generate-json-schema`), so the check spends nothing.
//
// What the panel uses is written in uses.txt, a place a line: the methods it
// calls, answers and hears, the fields of each it reads or sends, and the
// values it compares, maps or sends there. The reference is the schema of the
// release the panel was last checked against, pruned to what those places
// read, so it stays a few hundred definitions short of the dump. A check walks
// every place in the schema of the installed codex and in the reference and
// tells three lists apart:
//
//   - gone: a place, a value or a method the reference has and the schema
//     does not;
//   - changed: a place whose kind of value is another — a string that is now
//     a number — or an object the panel builds that now requires a field it
//     does not send;
//   - new: a value, a method or a notification the reference does not have.
//
// What the panel reads or sends that is gone or changed is red: the panel
// breaks on it. The rest is a warning — a new kind of an item the feed does
// not draw, a status word the screen shows as codex says it, a notification
// nobody turned off.
package contract

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

//go:embed uses.txt
var usesText string

//go:embed reference.json
var referenceJSON []byte

// ReferencePath is where the reference lives in the repository.
const ReferencePath = "internal/codex/contract/reference.json"

// BundleFile is the file of the schema bundle codex writes into its output
// directory: every definition in one document.
const BundleFile = "codex_app_server_protocol.schemas.json"

// versionKey is where the reference keeps the release it was taken from.
const versionKey = "x-codexVersion"

// Panel is the contract of the panel, as uses.txt writes it.
func Panel() (*Contract, error) { return Parse(usesText) }

// Reference is the schema the panel was last checked against.
func Reference() (*Schema, error) { return Load(referenceJSON) }

// ---------------------------------------------------------------- the contract

// The kinds of messages, as the schema groups them.
const (
	ClientRequest      = "client request"
	ServerRequest      = "server request"
	ServerNotification = "server notification"
	ClientNotification = "client notification"
)

// categories name the definition of the schema that lists the methods of a
// kind of message, in the order a report names them.
var categories = []struct{ kind, def string }{
	{ClientRequest, "ClientRequest"},
	{ServerRequest, "ServerRequest"},
	{ServerNotification, "ServerNotification"},
	{ClientNotification, "ClientNotification"},
}

// What the panel does with a method, as a line of uses.txt says it.
const (
	Call   = "call"
	Answer = "answer"
	Hear   = "hear"
	Tell   = "tell"
	Quiet  = "quiet"
)

var kindOf = map[string]string{Call: ClientRequest, Answer: ServerRequest, Hear: ServerNotification,
	Tell: ClientNotification}

// Contract is what the panel reads and sends of the protocol.
type Contract struct {
	Messages []*Message
	// Quiet are the notifications the panel turns off at the handshake.
	Quiet []string
}

// Message is a method the panel uses, and its places.
type Message struct {
	Use    string // call, answer, hear or tell
	Method string
	Places []*Place
}

// Kind is the kind of message the method is of.
func (m *Message) Kind() string { return kindOf[m.Use] }

// Place is one field of a message the panel reads or sends.
type Place struct {
	Send bool
	// Part is params or result: a call sends params and reads its result, an
	// answer reads params and sends its result, a notification has params.
	Part string
	Path string
	// Values are the words the panel compares, maps or sends there.
	Values []string
	steps  []step
}

// step is one step of a path: into a property, the items of a list, the
// values of a map, or the variant of a union a word names.
type step struct {
	op   byte // '.', '[', '{', '<'
	name string
}

// Label names a place in a report.
func (m *Message) Label(p *Place) string {
	return strings.TrimSpace(m.Method + " " + p.Part + " " + p.Path)
}

// Methods are the methods of a kind the panel uses.
func (c *Contract) Methods(kind string) []string {
	var out []string
	for _, m := range c.Messages {
		if m.Kind() == kind {
			out = append(out, m.Method)
		}
	}
	return out
}

// Values are the words the contract names at a place, nil when it names none.
func (c *Contract) Values(use, method, part, path string) []string {
	for _, m := range c.Messages {
		if m.Use != use || m.Method != method {
			continue
		}
		for _, p := range m.Places {
			if p.Part == part && p.Path == path {
				return p.Values
			}
		}
	}
	return nil
}

// Parse reads uses.txt.
func Parse(text string) (*Contract, error) {
	c := &Contract{}
	var cur *Message
	seen := map[string]bool{}
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		bad := func(format string, args ...any) error {
			return fmt.Errorf("uses.txt:%d: %s", n+1, fmt.Sprintf(format, args...))
		}
		fields := strings.Fields(trimmed)
		if line[0] != ' ' && line[0] != '\t' {
			switch fields[0] {
			case Quiet:
				if len(fields) < 2 {
					return nil, bad("quiet names no notification")
				}
				c.Quiet = append(c.Quiet, fields[1:]...)
				cur = nil
			case Call, Answer, Hear, Tell:
				if len(fields) != 2 {
					return nil, bad("%s takes one method", fields[0])
				}
				key := fields[0] + " " + fields[1]
				if seen[key] {
					return nil, bad("%s is written twice", key)
				}
				seen[key] = true
				cur = &Message{Use: fields[0], Method: fields[1]}
				c.Messages = append(c.Messages, cur)
			default:
				return nil, bad("%q is no message: call, answer, hear, tell or quiet", fields[0])
			}
			continue
		}
		if cur == nil {
			return nil, bad("a place under no message")
		}
		p, err := parsePlace(cur.Use, trimmed)
		if err != nil {
			return nil, bad("%v", err)
		}
		cur.Places = append(cur.Places, p)
	}
	return c, nil
}

func parsePlace(use, line string) (*Place, error) {
	word, rest, _ := strings.Cut(line, " ")
	p := &Place{}
	switch word {
	case "read":
	case "send":
		p.Send = true
	default:
		return nil, fmt.Errorf("%q is no place: read or send", word)
	}
	switch {
	case use == Tell:
		return nil, fmt.Errorf("a notification the panel sends has no places here")
	case use == Hear && p.Send:
		return nil, fmt.Errorf("a notification is only read")
	case use == Call && p.Send, use == Answer && !p.Send, use == Hear:
		p.Part = "params"
	default:
		p.Part = "result"
	}
	path, values, _ := strings.Cut(rest, ":")
	p.Path = strings.TrimSpace(path)
	p.Values = strings.Fields(values)
	steps, err := parsePath(p.Path)
	if err != nil {
		return nil, err
	}
	p.steps = steps
	return p, nil
}

func parsePath(path string) ([]step, error) {
	if path == "" {
		return nil, fmt.Errorf("a place names no field")
	}
	var out []step
	for _, part := range strings.Split(path, ".") {
		name := part
		if i := strings.IndexAny(part, "[{<"); i >= 0 {
			name, part = part[:i], part[i:]
		} else {
			part = ""
		}
		if name == "" {
			return nil, fmt.Errorf("%q: a step names no field", path)
		}
		out = append(out, step{op: '.', name: name})
		for part != "" {
			switch {
			case strings.HasPrefix(part, "[]"):
				out = append(out, step{op: '['})
				part = part[2:]
			case strings.HasPrefix(part, "{}"):
				out = append(out, step{op: '{'})
				part = part[2:]
			case strings.HasPrefix(part, "<"):
				end := strings.IndexByte(part, '>')
				if end < 2 {
					return nil, fmt.Errorf("%q: a variant names no word", path)
				}
				out = append(out, step{op: '<', name: part[1:end]})
				part = part[end+1:]
			default:
				return nil, fmt.Errorf("%q: %q is no step", path, part)
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- the schema

// Schema is a JSON Schema bundle as codex writes it, with a record of what was
// read of it: a walk marks every node it looks at, and a prune keeps those.
type Schema struct {
	root any
	// keep are the nodes kept whole: what a walk read of a node besides the
	// subschemas under it — its type, its values, what it requires, its
	// description. touched are the nodes on the way to them.
	keep    map[string]bool
	touched map[string]bool
}

// Load reads a schema bundle.
func Load(data []byte) (*Schema, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("the schema does not read: %w", err)
	}
	if _, ok := root.(map[string]any); !ok {
		return nil, fmt.Errorf("the schema is not an object")
	}
	return &Schema{root: root, keep: map[string]bool{}, touched: map[string]bool{}}, nil
}

// Version is the release of codex the schema is of, as the reference keeps
// it; empty for a schema codex has just written.
func (s *Schema) Version() string {
	v, _ := s.root.(map[string]any)[versionKey].(string)
	return v
}

// A pointer names a node the way JSON Pointer does: "/definitions/v2/Thread".
func join(ptr string, key any) string {
	k := fmt.Sprint(key)
	k = strings.ReplaceAll(k, "~", "~0")
	k = strings.ReplaceAll(k, "/", "~1")
	return ptr + "/" + k
}

func (s *Schema) at(ptr string) any {
	node := s.root
	if ptr == "" {
		return node
	}
	for _, tok := range strings.Split(ptr[1:], "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch n := node.(type) {
		case map[string]any:
			node = n[tok]
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(n) {
				return nil
			}
			node = n[i]
		default:
			return nil
		}
	}
	return node
}

func (s *Schema) touch(ptr string) {
	for ptr != "" && !s.touched[ptr] {
		s.touched[ptr] = true
		ptr = ptr[:strings.LastIndexByte(ptr, '/')]
	}
}

// containers are the keywords whose value holds subschemas: a walk goes into
// them member by member, and a prune keeps only the members it went into.
var containers = map[string]bool{"properties": true, "items": true, "additionalProperties": true,
	"allOf": true, "anyOf": true, "oneOf": true, "definitions": true}

// look marks a node read: everything of it but its subschemas, and that it
// has them — an object that names its properties is an object even when none
// of them is read.
func (s *Schema) look(ptr string) {
	s.touch(ptr)
	obj, ok := s.at(ptr).(map[string]any)
	if !ok {
		s.keep[ptr] = true
		return
	}
	for k, v := range obj {
		switch v.(type) {
		case map[string]any, []any:
			if containers[k] {
				s.touch(join(ptr, k))
				continue
			}
		}
		s.keep[join(ptr, k)] = true
	}
}

// alts are the schemas a node stands for: itself where it says something of
// its own, and what its $ref, allOf, anyOf and oneOf lead to. A field that is
// a string or null is two alternatives; a union of kinds is one per kind.
func (s *Schema) alts(ptr string) []string {
	return s.expand(ptr, map[string]bool{})
}

func (s *Schema) expand(ptr string, seen map[string]bool) []string {
	if seen[ptr] {
		return nil
	}
	seen[ptr] = true
	s.look(ptr)
	obj, ok := s.at(ptr).(map[string]any)
	if !ok {
		return []string{ptr}
	}
	var out []string
	if ref, ok := obj["$ref"].(string); ok && strings.HasPrefix(ref, "#") {
		out = append(out, s.expand(ref[1:], seen)...)
	}
	for _, kw := range []string{"allOf", "anyOf", "oneOf"} {
		list, _ := obj[kw].([]any)
		for i := range list {
			out = append(out, s.expand(join(join(ptr, kw), i), seen)...)
		}
	}
	own := false
	for _, k := range []string{"type", "properties", "items", "additionalProperties", "enum", "const"} {
		if _, ok := obj[k]; ok {
			own = true
		}
	}
	if own || len(out) == 0 {
		out = append([]string{ptr}, out...)
	}
	return out
}

func (s *Schema) object(ptr string) map[string]any {
	obj, _ := s.at(ptr).(map[string]any)
	return obj
}

// types are the kinds of JSON value the alternatives take, null aside: Go
// reads a null into any field as nothing, so a field turned nullable or not
// changes nothing the panel decodes. The price: a field the panel sends a null
// in that stops taking one is not told.
func (s *Schema) types(alts []string) []string {
	set := map[string]bool{}
	for _, a := range alts {
		obj := s.object(a)
		if obj == nil {
			if b, ok := s.at(a).(bool); ok && b {
				set["any"] = true
			}
			continue
		}
		switch t := obj["type"].(type) {
		case string:
			set[t] = true
			continue
		case []any:
			for _, x := range t {
				set[fmt.Sprint(x)] = true
			}
			continue
		}
		switch {
		case obj["properties"] != nil:
			set["object"] = true
		case obj["items"] != nil:
			set["array"] = true
		case obj["enum"] != nil || obj["const"] != nil:
			set["string"] = true
		case obj["$ref"] == nil && obj["allOf"] == nil && obj["anyOf"] == nil && obj["oneOf"] == nil:
			set["any"] = true
		}
	}
	delete(set, "null")
	return sorted(set)
}

// values are the words the alternatives allow: the members of an enum, a
// constant, and the key of a variant that is an object of that one key — the
// way codex writes a decision that carries an amendment.
func (s *Schema) values(alts []string) []string {
	set := map[string]bool{}
	for _, a := range alts {
		obj := s.object(a)
		if obj == nil {
			continue
		}
		if list, ok := obj["enum"].([]any); ok {
			for _, v := range list {
				set[word(v)] = true
			}
		}
		if v, ok := obj["const"]; ok {
			set[word(v)] = true
		}
		if key := s.tag(a); key != "" {
			set[key] = true
		}
	}
	return sorted(set)
}

// tag is the one key of a variant that is an object of that key alone. The
// names of its properties are what tells it, so they are marked read: a
// reference that dropped the others would take an object of several keys for
// a variant of one.
func (s *Schema) tag(ptr string) string {
	obj := s.object(ptr)
	req, _ := obj["required"].([]any)
	if more, ok := obj["additionalProperties"].(bool); !ok || more || len(req) != 1 {
		return ""
	}
	props, _ := obj["properties"].(map[string]any)
	for name := range props {
		s.touch(join(join(ptr, "properties"), name))
	}
	key := fmt.Sprint(req[0])
	if _, ok := props[key]; !ok || len(props) != 1 {
		return ""
	}
	return key
}

func word(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// required are the fields every alternative that is an object requires.
func (s *Schema) required(alts []string) []string {
	var out []string
	first := true
	for _, a := range alts {
		obj := s.object(a)
		if obj == nil || !slices.Contains(s.types([]string{a}), "object") {
			continue
		}
		var names []string
		list, _ := obj["required"].([]any)
		for _, v := range list {
			names = append(names, fmt.Sprint(v))
		}
		if first {
			out, first = names, false
			continue
		}
		out = slices.DeleteFunc(out, func(n string) bool { return !slices.Contains(names, n) })
	}
	sort.Strings(out)
	return out
}

// picks says an alternative is the variant of a union a word names: its type
// is that word, it is an object of that one key, or it is that word itself.
func (s *Schema) picks(alt, name string) bool {
	obj := s.object(alt)
	if obj == nil {
		return false
	}
	if props, ok := obj["properties"].(map[string]any); ok {
		if _, ok := props["type"]; ok {
			if slices.Contains(s.values(s.alts(join(join(alt, "properties"), "type"))), name) {
				return true
			}
		}
	}
	if s.tag(alt) == name {
		return true
	}
	if list, ok := obj["enum"].([]any); ok {
		return slices.ContainsFunc(list, func(v any) bool { return word(v) == name })
	}
	return false
}

// walk follows a path from a node and returns where it leads, or the step at
// which the schema has nothing to follow.
func (s *Schema) walk(ptr string, steps []step) ([]string, string) {
	ptrs := []string{ptr}
	for i, st := range steps {
		var next []string
		for _, p := range ptrs {
			for _, a := range s.alts(p) {
				obj := s.object(a)
				if obj == nil {
					continue
				}
				switch st.op {
				case '.':
					if props, ok := obj["properties"].(map[string]any); ok {
						if _, ok := props[st.name]; ok {
							next = append(next, join(join(a, "properties"), st.name))
						}
					}
				case '[':
					if _, ok := obj["items"].(map[string]any); ok {
						next = append(next, join(a, "items"))
					}
				case '{':
					switch more := obj["additionalProperties"].(type) {
					case map[string]any:
						next = append(next, join(a, "additionalProperties"))
					case bool:
						if more {
							next = append(next, join(a, "additionalProperties"))
						}
					}
				case '<':
					if s.picks(a, st.name) {
						next = append(next, a)
					}
				}
			}
		}
		if len(next) == 0 {
			return nil, stepText(steps[:i+1])
		}
		ptrs = dedup(next)
	}
	var out []string
	for _, p := range ptrs {
		out = append(out, s.alts(p)...)
	}
	return dedup(out), ""
}

func stepText(steps []step) string {
	var b strings.Builder
	for _, st := range steps {
		switch st.op {
		case '.':
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(st.name)
		case '[':
			b.WriteString("[]")
		case '{':
			b.WriteString("{}")
		case '<':
			b.WriteString("<" + st.name + ">")
		}
	}
	return b.String()
}

// methods are the methods of a kind of message the schema lists, each with
// the node of its params.
func (s *Schema) methods(def string) map[string]string {
	out := map[string]string{}
	base := join("/definitions", def)
	list, _ := s.object(base)["oneOf"].([]any)
	for i := range list {
		member := join(join(base, "oneOf"), i)
		method := join(join(member, "properties"), "method")
		if s.object(method) == nil {
			continue
		}
		// Only the name is kept of a method the panel does not use: the
		// reference lists the methods so that a check can say which are new.
		for _, k := range []string{"enum", "const"} {
			if _, ok := s.object(method)[k]; ok {
				s.keep[join(method, k)] = true
				s.touch(join(method, k))
			}
		}
		for _, name := range s.values([]string{method}) {
			out[name] = join(join(member, "properties"), "params")
		}
	}
	return out
}

// result is the node of the answer to a request: the definition named after
// its params, XResponse for XParams, beside it. The schema does not tie a
// request to its answer otherwise.
func (s *Schema) result(params string) string {
	s.look(params)
	var refs []string
	obj := s.object(params)
	if ref, ok := obj["$ref"].(string); ok {
		refs = append(refs, ref)
	}
	// Params a request may go without are a union of their definition and null.
	for _, kw := range []string{"anyOf", "oneOf"} {
		list, _ := obj[kw].([]any)
		for i := range list {
			member := join(join(params, kw), i)
			s.look(member)
			if ref, ok := s.object(member)["$ref"].(string); ok {
				refs = append(refs, ref)
			}
		}
	}
	for _, ref := range refs {
		if base, ok := strings.CutSuffix(ref, "Params"); ok && strings.HasPrefix(base, "#") {
			ptr := base[1:] + "Response"
			if s.at(ptr) != nil {
				return ptr
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------- the facts

// fact is what a schema says of a place.
type fact struct {
	found bool
	// missing is the part of the path the schema has nothing at.
	missing  string
	types    []string
	values   []string
	required []string
}

// facts are what a schema says of every place and method of the contract.
type facts struct {
	methods map[string]map[string]bool // kind → method
	places  map[string]fact            // label → fact
	// builds are the objects the panel builds field by field, by label, with
	// what the schema requires of each.
	builds map[string][]string
}

func (s *Schema) facts(c *Contract) facts {
	f := facts{methods: map[string]map[string]bool{}, places: map[string]fact{}, builds: map[string][]string{}}
	s.look("")
	params := map[string]map[string]string{}
	for _, cat := range categories {
		params[cat.kind] = s.methods(cat.def)
		f.methods[cat.kind] = map[string]bool{}
		for m := range params[cat.kind] {
			f.methods[cat.kind][m] = true
		}
	}
	for _, m := range c.Messages {
		ptr, ok := params[m.Kind()][m.Method]
		if !ok {
			continue
		}
		parts := map[string]string{"params": ptr}
		if m.Use == Call || m.Use == Answer {
			parts["result"] = s.result(ptr)
		}
		for _, p := range m.Places {
			root := parts[p.Part]
			if root == "" || s.at(root) == nil {
				f.places[m.Label(p)] = fact{missing: p.Part}
				continue
			}
			alts, missing := s.walk(root, p.steps)
			if missing != "" {
				f.places[m.Label(p)] = fact{missing: missing}
				continue
			}
			f.places[m.Label(p)] = fact{found: true, types: s.types(alts), values: s.values(alts)}
		}
		for _, b := range builds(m) {
			root := parts[b.part]
			if root == "" || s.at(root) == nil {
				continue
			}
			alts, missing := s.walk(root, b.steps)
			if missing != "" {
				continue
			}
			f.builds[b.label] = s.required(alts)
		}
	}
	return f
}

// build is an object the panel builds field by field: the params of a call,
// the answer to a request, and every object on the way to a field it sends.
// An object sent as it came — permissions granted as they were asked — is not
// one: the panel does not choose its fields.
type build struct {
	label  string
	part   string
	steps  []step
	fields []string
}

func builds(m *Message) []build {
	byLabel := map[string]*build{}
	var order []string
	for _, p := range m.Places {
		if !p.Send {
			continue
		}
		for i := range p.steps {
			if p.steps[i].op != '.' {
				continue
			}
			prefix := p.steps[:i]
			label := strings.TrimSpace(m.Method + " " + p.Part + " " + stepText(prefix))
			b := byLabel[label]
			if b == nil {
				b = &build{label: label, part: p.Part, steps: prefix}
				byLabel[label] = b
				order = append(order, label)
			}
			if !slices.Contains(b.fields, p.steps[i].name) {
				b.fields = append(b.fields, p.steps[i].name)
			}
		}
	}
	out := make([]build, 0, len(order))
	for _, label := range order {
		out = append(out, *byLabel[label])
	}
	return out
}

// ---------------------------------------------------------------- the check

// Finding is one line of a report.
type Finding struct {
	Place  string `json:"place"`
	Detail string `json:"detail"`
	// Red says the panel breaks on it.
	Red bool `json:"red"`
}

// Report is what a check found: the release checked and the release of the
// reference, and the three lists.
type Report struct {
	Codex     string    `json:"codex"`
	Reference string    `json:"reference"`
	Gone      []Finding `json:"gone"`
	Changed   []Finding `json:"changed"`
	New       []Finding `json:"new"`
}

// Red says something the panel reads or sends is gone or changed.
func (r Report) Red() bool {
	for _, list := range [][]Finding{r.Gone, r.Changed, r.New} {
		if slices.ContainsFunc(list, func(f Finding) bool { return f.Red }) {
			return true
		}
	}
	return false
}

// Check compares a schema with the reference over what the contract uses.
func Check(cur, ref *Schema, c *Contract) Report {
	now, was := cur.facts(c), ref.facts(c)
	r := Report{Codex: cur.Version(), Reference: ref.Version(), Gone: []Finding{}, Changed: []Finding{},
		New: []Finding{}}
	r.methods(now, was, c)
	for _, m := range c.Messages {
		for _, p := range m.Places {
			r.place(m.Label(p), p, now.places[m.Label(p)], was.places[m.Label(p)])
		}
		for _, b := range builds(m) {
			need, ok := now.builds[b.label]
			if !ok {
				continue
			}
			for _, field := range need {
				if !slices.Contains(b.fields, field) {
					r.Changed = append(r.Changed, Finding{Place: b.label, Red: true,
						Detail: "requires " + field + ", which the panel does not send"})
				}
			}
		}
	}
	return r
}

func (r *Report) methods(now, was facts, c *Contract) {
	for _, cat := range categories {
		used := c.Methods(cat.kind)
		for _, m := range sorted(was.methods[cat.kind]) {
			if now.methods[cat.kind][m] {
				continue
			}
			switch {
			case slices.Contains(used, m):
				// Told below, with the method the panel uses.
			case cat.kind == ServerNotification && slices.Contains(c.Quiet, m):
				r.Gone = append(r.Gone, Finding{Place: cat.kind + " " + m, Detail: "the panel turns it off"})
			default:
				r.Gone = append(r.Gone, Finding{Place: cat.kind + " " + m, Detail: "the panel does not use it"})
			}
		}
		for _, m := range used {
			if !now.methods[cat.kind][m] {
				r.Gone = append(r.Gone, Finding{Place: cat.kind + " " + m, Red: true, Detail: "the panel uses it"})
			}
		}
		for _, m := range sorted(now.methods[cat.kind]) {
			if was.methods[cat.kind][m] {
				continue
			}
			detail := "new"
			switch {
			case slices.Contains(used, m):
				detail = "new, and the panel uses it"
			case cat.kind == ServerNotification && slices.Contains(c.Quiet, m):
				detail = "new, and the panel turns it off"
			case cat.kind == ServerNotification:
				detail = "new, neither read nor turned off: it reaches the executor unread — add it to the quiet list"
			case cat.kind == ServerRequest:
				detail = "new: the panel does not answer it"
			}
			r.New = append(r.New, Finding{Place: cat.kind + " " + m, Detail: detail})
		}
	}
}

func (r *Report) place(label string, p *Place, now, was fact) {
	use := "reads"
	if p.Send {
		use = "sends"
	}
	if !now.found {
		detail := "the panel " + use + " it"
		if now.missing != "" && now.missing != p.Path {
			detail += "; the schema has nothing at " + now.missing
		}
		if now.missing == "" {
			// The method itself is gone, and the method says it.
			return
		}
		r.Gone = append(r.Gone, Finding{Place: label, Red: true, Detail: detail})
		return
	}
	if !was.found {
		r.New = append(r.New, Finding{Place: label, Detail: "the reference does not have it: write the reference again"})
		return
	}
	if !slices.Equal(now.types, was.types) {
		r.Changed = append(r.Changed, Finding{Place: label, Red: true,
			Detail: fmt.Sprintf("%s, was %s; the panel %s it", strings.Join(now.types, " or "),
				strings.Join(was.types, " or "), use)})
	}
	if p.Values == nil {
		return
	}
	for _, v := range p.Values {
		if !slices.Contains(now.values, v) {
			r.Gone = append(r.Gone, Finding{Place: label, Red: true, Detail: fmt.Sprintf("%q, which the panel %s", v, use)})
		}
	}
	for _, v := range was.values {
		if !slices.Contains(now.values, v) && !slices.Contains(p.Values, v) {
			r.Gone = append(r.Gone, Finding{Place: label, Detail: fmt.Sprintf("%q, which the panel does not name", v)})
		}
	}
	for _, v := range now.values {
		if !slices.Contains(was.values, v) {
			r.New = append(r.New, Finding{Place: label, Detail: fmt.Sprintf("%q", v)})
		}
	}
}

// ---------------------------------------------------------------- the reference

// Prune is the schema cut to what the contract reads of it, with the release
// it is of: the reference a check compares the next release with.
func Prune(s *Schema, c *Contract, version string) ([]byte, error) {
	s.keep, s.touched = map[string]bool{}, map[string]bool{}
	s.facts(c)
	out, _ := s.prune("", s.root).(map[string]any)
	out[versionKey] = version
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Schema) prune(ptr string, node any) any {
	if s.keep[ptr] {
		return node
	}
	switch n := node.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range n {
			child := join(ptr, k)
			if s.keep[child] || s.touched[child] {
				out[k] = s.prune(child, v)
			}
		}
		return out
	case []any:
		// A member of a union is named by its place in the list: every one
		// stays, and one nothing was read of stays empty.
		out := make([]any, len(n))
		for i, v := range n {
			child := join(ptr, i)
			if s.keep[child] || s.touched[child] {
				out[i] = s.prune(child, v)
			} else {
				out[i] = map[string]any{}
			}
		}
		return out
	}
	return node
}

// ---------------------------------------------------------------- the words

// Text is the report as a person reads it: the three lists, a red line marked.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s, against the reference of %s\n", orUnknown(r.Codex), orUnknown(r.Reference))
	for _, list := range []struct {
		name  string
		found []Finding
	}{{"gone", r.Gone}, {"changed", r.Changed}, {"new", r.New}} {
		if len(list.found) == 0 {
			fmt.Fprintf(&b, "%s: nothing\n", list.name)
			continue
		}
		fmt.Fprintf(&b, "%s:\n", list.name)
		for _, f := range list.found {
			mark := "  "
			if f.Red {
				mark = "!!"
			}
			fmt.Fprintf(&b, "  %s %s — %s\n", mark, f.Place, f.Detail)
		}
	}
	return b.String()
}

func orUnknown(v string) string {
	if v == "" {
		return "an unnamed release"
	}
	return v
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedup(list []string) []string {
	seen := map[string]bool{}
	out := list[:0:0]
	for _, v := range list {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
