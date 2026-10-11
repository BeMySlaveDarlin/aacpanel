package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---- JSON as python's json module reads and writes it ----
//
// The question relay and the page copy hand the collector a message that the
// delivery's scripts wrote with json.dumps(…, ensure_ascii=False), and the
// executor's hooks write it the same, byte for byte: the keys of an object in
// the order they came, a key given twice where it first came with the value it
// was last given, ", " and ": " between the parts, a number as python writes
// it back and a string with only the escapes python makes. The collector reads
// either the same, and the message is as long as before against its ceiling.
//
// What python's reader takes and Go's does not — NaN and Infinity among the
// numbers, an object nested past a thousand levels, which python fails on
// — claude never writes.

// pyObject is an object as python reads it into a dict: each key where it
// first came, with the value it was last given.
type pyObject struct {
	keys []string
	vals map[string]any
}

// get is the value of a key, as dict.get gives it: nil for a key not there.
func (o *pyObject) get(key string) any {
	if o == nil {
		return nil
	}
	return o.vals[key]
}

// pyLoad reads a JSON text the way python's json.load reads it from claude's
// stdin: objects as *pyObject, lists as []any, numbers as json.Number. A text
// that is not one JSON value, or not UTF-8, which python's stdin refuses, is
// false.
func pyLoad(raw []byte) (any, bool) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := pyValue(dec)
	return v, err == nil
}

func pyValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t {
	case json.Delim('{'):
		o := &pyObject{vals: map[string]any{}}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := k.(string)
			v, err := pyValue(dec)
			if err != nil {
				return nil, err
			}
			if _, seen := o.vals[key]; !seen {
				o.keys = append(o.keys, key)
			}
			o.vals[key] = v
		}
		_, err := dec.Token()
		return o, err
	case json.Delim('['):
		l := []any{}
		for dec.More() {
			v, err := pyValue(dec)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}
		_, err := dec.Token()
		return l, err
	}
	return t, nil
}

// pyDumps is json.dumps(v, ensure_ascii=False) of a value pyLoad read or a
// hook made of strings.
func pyDumps(v any) []byte {
	var b bytes.Buffer
	pyWrite(&b, v)
	return b.Bytes()
}

func pyWrite(b *bytes.Buffer, v any) {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		pyQuote(b, v)
	case json.Number:
		b.WriteString(pyNumber(v))
	case *pyObject:
		b.WriteByte('{')
		for i, k := range v.keys {
			if i > 0 {
				b.WriteString(", ")
			}
			pyQuote(b, k)
			b.WriteString(": ")
			pyWrite(b, v.vals[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			pyWrite(b, e)
		}
		b.WriteByte(']')
	}
}

// pyNumber is a number as python writes back what it read: a whole number
// as an int, with its every digit, anything else as a float, and a float past
// the range as Infinity.
func pyNumber(n json.Number) string {
	f, _ := number(n)
	switch {
	case strings.ContainsAny(string(n), ".eE") && math.IsInf(f, 1):
		return "Infinity"
	case strings.ContainsAny(string(n), ".eE") && math.IsInf(f, -1):
		return "-Infinity"
	}
	return pyStr(n)
}

// pyQuote writes a string as json.dumps quotes it with ensure_ascii off: the
// quote, the backslash and the control characters escaped, \b \f \n \r \t by
// their letters and the rest as \u00xx, everything else as it is.
func pyQuote(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// pySpace is a character python's str.split and str.strip take for white
// space: Go's own, and the four separators of files, groups, records and
// units besides.
func pySpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f
}

// pyText is str(value or ""): a value that says nothing is empty, a string
// is itself, and a number or true is what python's str writes. A list or an
// object, which python writes as its repr, comes as its JSON: no field the
// hooks read this way holds one.
func pyText(v any) string {
	if !pyTruthy(v) {
		return ""
	}
	if o, ok := v.(*pyObject); ok {
		return string(pyDumps(o))
	}
	return pyStr(v)
}

// pyTruthy is truthy for a value pyLoad read.
func pyTruthy(v any) bool {
	if o, ok := v.(*pyObject); ok {
		return len(o.keys) > 0
	}
	return truthy(v)
}
