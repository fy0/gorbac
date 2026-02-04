package filter

import "reflect"

// Bindings provides runtime values for CEL variables which are not schema fields.
//
// These values are used when rendering SQL placeholders and when evaluating
// compiled conditions in-memory.
type Bindings map[string]any

func toAnySlice(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}

	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, true
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}

	n := rv.Len()
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out, true
}
