package schema

// Layers an effective value comes from, weakest first.
const (
	// LayerClaude: nothing sets it, and the parameter's Unset says what
	// happens then.
	LayerClaude = "claude"
	// LayerAccount: the settings.json of the contour's account. The panel
	// reads it and never writes it; it reaches the session past the command.
	LayerAccount = "account"
	LayerContour = "contour"
	LayerProject = "project"
)

// Value is a parameter as a launch takes it, with the layer that gave it.
// Value is null where no layer sets the parameter: the screen then says the
// Unset of the schema. A false or an empty text is a value like any other.
type Value struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	Layer string `json:"layer"`
	// From names the layer of each key of a value merged key by key.
	From map[string]string `json:"from,omitempty"`
}

// Effective lays a project's launch parameters over its contour's, and both
// over the account's settings: one value per parameter of the schema, in its
// order, each with the layer that gave it. A nil layer is an empty one.
func Effective(account, contour, project map[string]any) []Value {
	out := make([]Value, 0, len(params))
	for _, p := range params {
		v := Value{Key: p.Key, Layer: LayerClaude}
		for _, layer := range []struct {
			name string
			obj  map[string]any
		}{{LayerAccount, account}, {LayerContour, contour}, {LayerProject, project}} {
			got, ok := layer.obj[p.Key]
			if !ok {
				continue
			}
			if p.Merge == MergeByKey {
				v = mergeKeys(v, got, layer.name)
				continue
			}
			v.Value, v.Layer = got, layer.name
		}
		out = append(out, v)
	}
	return out
}

func mergeKeys(v Value, got any, layer string) Value {
	over, ok := got.(map[string]any)
	if !ok {
		return v
	}
	base, _ := v.Value.(map[string]any)
	merged := make(map[string]any, len(base)+len(over))
	from := make(map[string]string, len(base)+len(over))
	for k, x := range base {
		merged[k], from[k] = x, v.From[k]
	}
	for k, x := range over {
		merged[k], from[k] = x, layer
	}
	return Value{Key: v.Key, Value: merged, Layer: layer, From: from}
}

// Launch returns what the launcher is given for a project: the contour's
// parameters with the project's laid over them. The account's settings are
// not among them — claude reads those itself, and passing them as flags would
// pin a project to what the account said on the day of the launch. Keys the
// schema does not hold are dropped: the map refuses them on save, and the
// launcher would only name them.
func Launch(contour, project map[string]any) map[string]any {
	out := map[string]any{}
	for _, v := range Effective(nil, contour, project) {
		if v.Layer != LayerClaude {
			out[v.Key] = v.Value
		}
	}
	return out
}
