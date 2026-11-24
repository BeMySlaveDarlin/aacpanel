package mcp

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
)

// Strict-schema clients represent omitted optionals with null. Copy only the
// schema containers we change: tools share their maps with other callers.
func nullableSchema(schema map[string]any) map[string]any {
	out := maps.Clone(schema)
	for key, value := range schema {
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			if entries, ok := value.(map[string]any); ok {
				copied := maps.Clone(entries)
				for name, entry := range entries {
					copied[name] = nullableSubschema(entry)
					if key == "properties" && !slices.Contains(schemaArray(schema["required"]), any(name)) {
						copied[name] = allowNull(copied[name])
					}
				}
				out[key] = copied
			}
		case "items", "additionalItems", "additionalProperties", "contains", "propertyNames",
			"not", "if", "then", "else", "unevaluatedItems", "unevaluatedProperties":
			out[key] = nullableSubschema(value)
		case "allOf", "anyOf", "oneOf", "prefixItems":
			out[key] = nullableSubschema(value)
		}
	}
	return out
}

func nullableSubschema(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return nullableSchema(value)
	case []any:
		out := make([]any, len(value))
		for i, entry := range value {
			out[i] = nullableSubschema(entry)
		}
		return out
	default:
		return value
	}
}

func allowNull(value any) any {
	if schema, ok := value.(map[string]any); ok {
		typed, enumerated := false, false
		if kind, ok := schema["type"].(string); ok {
			schema["type"] = appendSchemaValue([]any{kind}, "null")
			typed = true
		} else if kinds := schemaArray(schema["type"]); kinds != nil {
			schema["type"] = appendSchemaValue(kinds, "null")
			typed = true
		}
		if values := schemaArray(schema["enum"]); values != nil {
			schema["enum"] = appendSchemaValue(values, nil)
			enumerated = true
		}
		// A const, reference or composition can reject null independently of
		// type and enum. Keep its constraints together in the non-null branch.
		wrap := !typed && !enumerated
		for _, key := range []string{"const", "$ref", "allOf", "anyOf", "oneOf", "not", "if"} {
			if _, exists := schema[key]; exists {
				wrap = true
			}
		}
		if !wrap {
			return schema
		}
	}
	return map[string]any{"anyOf": []any{value, map[string]any{"type": "null"}}}
}

// Go-authored schemas use []string; decoded schemas use []any. Neither slice
// may be appended to in place, even when its spare capacity would allow it.
func schemaArray(value any) []any {
	switch value := value.(type) {
	case []any:
		return value
	case []string:
		out := make([]any, len(value))
		for i, entry := range value {
			out[i] = entry
		}
		return out
	default:
		return nil
	}
}

func appendSchemaValue(values []any, value any) []any {
	if slices.Contains(values, value) {
		return values
	}
	return append(slices.Clone(values), value)
}

// Raw messages keep numbers exact and null array elements in their positions;
// only object members stand for optional arguments that can be omitted.
func withoutNullMembers(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, nil
	}
	changed := false
	switch trimmed[0] {
	case '{':
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			return nil, err
		}
		for key, value := range members {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				delete(members, key)
				changed = true
				continue
			}
			cleaned, err := withoutNullMembers(value)
			if err != nil {
				return nil, err
			}
			members[key] = cleaned
			changed = changed || !bytes.Equal(value, cleaned)
		}
		if changed {
			return json.Marshal(members)
		}
	case '[':
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
		for i, value := range entries {
			cleaned, err := withoutNullMembers(value)
			if err != nil {
				return nil, err
			}
			entries[i] = cleaned
			changed = changed || !bytes.Equal(value, cleaned)
		}
		if changed {
			return json.Marshal(entries)
		}
	}
	return raw, nil
}
