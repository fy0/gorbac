package filter

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/google/cel-go/cel"
)

var timeType = reflect.TypeOf(time.Time{})

// SchemaFromStruct builds a Schema from a Go struct type using reflection.
//
// It is intended as a convenience helper to reduce boilerplate when the filter
// schema matches a DB model struct.
//
// Supported Go field types:
//   - string / *string        -> FieldTypeString
//   - bool / *bool            -> FieldTypeBool
//   - int/uint variants       -> FieldTypeInt
//   - time.Time / *time.Time  -> FieldTypeTimestamp (represented as unix seconds in CEL)
//
// Field name resolution precedence:
//  1. `filter` tag (first segment, json-style)
//  2. `json` tag
//  3. `db` tag
//  4. snake_case of Go field name
//
// Column name resolution precedence:
//  1. `filter` tag option `column=...`
//  2. `db` tag
//  3. `gorm` tag option `column:...`
//  4. resolved field name
//
// The `filter` tag supports:
//   - "-" to skip the field
//   - "contains" to enable <field>.contains(x)
//   - "kind=..." to set FieldKind (scalar/json_bool/json_list/virtual_alias)
//   - "json=..." to set JSONPath (dot or slash separated)
//   - "alias=..." / "alias_for=..." to set AliasFor for virtual aliases
//   - "ops=..." to set AllowedComparisonOps (pipe separated; eq|neq|lt|lte|gt|gte)
//
// Returned EnvOptions only include CEL variables for schema fields; you can
// append extra variables (bindings) as needed.
func SchemaFromStruct(name, table string, model any) (Schema, error) {
	if strings.TrimSpace(table) == "" {
		return Schema{}, fmt.Errorf("table is required")
	}

	rt, err := normalizeStructType(model)
	if err != nil {
		return Schema{}, err
	}

	if strings.TrimSpace(name) == "" {
		base := rt.Name()
		if base == "" {
			return Schema{}, fmt.Errorf("schema name is required for anonymous structs")
		}
		name = snakeCase(base)
	}

	fields := map[string]*Field{}
	envOptions := make([]cel.EnvOption, 0, rt.NumField())
	if err := collectFieldsFromStruct(rt, table, fields, &envOptions); err != nil {
		return Schema{}, err
	}

	return Schema{
		Name:       name,
		Fields:     fields,
		EnvOptions: envOptions,
	}, nil
}

func normalizeStructType(model any) (reflect.Type, error) {
	if model == nil {
		return nil, fmt.Errorf("model is nil")
	}

	var rt reflect.Type
	if t, ok := model.(reflect.Type); ok {
		rt = t
	} else {
		rt = reflect.TypeOf(model)
	}

	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct (or pointer to struct), got %s", rt.Kind())
	}
	return rt, nil
}

type parsedFilterTag struct {
	skip             bool
	explicit         bool
	name             string
	kind             FieldKind
	table            string
	column           string
	jsonPath         []string
	aliasFor         string
	supportsContains bool
	allowedOps       map[ComparisonOperator]bool
}

func parseFilterTag(raw string) parsedFilterTag {
	if raw == "" {
		return parsedFilterTag{}
	}

	out := parsedFilterTag{explicit: true}
	parts := strings.Split(raw, ",")
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "-" {
			out.skip = true
			return out
		}
		if idx == 0 && !strings.Contains(part, "=") && part != "contains" {
			out.name = part
			continue
		}

		switch {
		case part == "contains":
			out.supportsContains = true
		case strings.HasPrefix(part, "kind="):
			out.kind = FieldKind(strings.TrimPrefix(part, "kind="))
		case strings.HasPrefix(part, "table="):
			out.table = strings.TrimPrefix(part, "table=")
		case strings.HasPrefix(part, "column="):
			out.column = strings.TrimPrefix(part, "column=")
		case strings.HasPrefix(part, "json="):
			path := strings.TrimPrefix(part, "json=")
			path = strings.Trim(path, ".")
			path = strings.Trim(path, "/")
			if path == "" {
				continue
			}
			splitter := func(r rune) bool { return r == '.' || r == '/' }
			out.jsonPath = strings.FieldsFunc(path, splitter)
		case strings.HasPrefix(part, "alias_for="):
			out.aliasFor = strings.TrimPrefix(part, "alias_for=")
		case strings.HasPrefix(part, "alias="):
			out.aliasFor = strings.TrimPrefix(part, "alias=")
		case strings.HasPrefix(part, "ops="):
			spec := strings.TrimPrefix(part, "ops=")
			out.allowedOps = parseComparisonOps(spec)
		}
	}

	return out
}

func parseComparisonOps(spec string) map[ComparisonOperator]bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}

	out := map[ComparisonOperator]bool{}
	for _, raw := range strings.Split(spec, "|") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		switch raw {
		case "eq", "==":
			out[CompareEq] = true
		case "neq", "!=":
			out[CompareNeq] = true
		case "lt", "<":
			out[CompareLt] = true
		case "lte", "<=":
			out[CompareLte] = true
		case "gt", ">":
			out[CompareGt] = true
		case "gte", ">=":
			out[CompareGte] = true
		}
	}
	return out
}

func collectFieldsFromStruct(rt reflect.Type, defaultTable string, fields map[string]*Field, envOptions *[]cel.EnvOption) error {
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)

		// Skip unexported fields unless they are anonymous (embedded) structs.
		if sf.PkgPath != "" && !sf.Anonymous {
			continue
		}

		filterTagRaw, filterTagPresent := sf.Tag.Lookup("filter")
		tag := parseFilterTag(filterTagRaw)
		if tag.skip {
			continue
		}

		fieldType := sf.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}

		// Flatten embedded structs by default.
		if sf.Anonymous && fieldType.Kind() == reflect.Struct && fieldType != timeType && !filterTagPresent {
			if err := collectFieldsFromStruct(fieldType, defaultTable, fields, envOptions); err != nil {
				return err
			}
			continue
		}

		name := tag.name
		if name == "" {
			name = pickTagName(sf.Tag.Get("json"))
		}
		if name == "" {
			name = pickTagName(sf.Tag.Get("db"))
		}
		if name == "" {
			name = snakeCase(sf.Name)
		}
		if name == "-" {
			continue
		}

		kind := tag.kind
		if kind == "" {
			kind = FieldKindScalar
		}

		ft, err := inferFieldType(sf.Type, kind)
		if err != nil {
			if tag.explicit {
				return fmt.Errorf("field %s: %w", sf.Name, err)
			}
			continue
		}

		def := &Field{
			Name:             name,
			Kind:             kind,
			Type:             ft,
			SupportsContains: tag.supportsContains,
		}

		switch kind {
		case FieldKindVirtualAlias:
			if strings.TrimSpace(tag.aliasFor) == "" {
				return fmt.Errorf("field %s: virtual_alias requires alias=... (target field name)", sf.Name)
			}
			def.AliasFor = tag.aliasFor
		default:
			column := tag.column
			if column == "" {
				column = pickTagName(sf.Tag.Get("db"))
			}
			if column == "" {
				column = pickGormColumn(sf.Tag.Get("gorm"))
			}
			if column == "" {
				column = name
			}

			colTable := tag.table
			if colTable == "" {
				colTable = defaultTable
			}

			def.Column = Column{
				Table: colTable,
				Name:  column,
			}
		}

		switch kind {
		case FieldKindJSONBool:
			if ft != FieldTypeBool {
				return fmt.Errorf("field %s: json_bool requires bool type", sf.Name)
			}
			if len(tag.jsonPath) == 0 {
				return fmt.Errorf("field %s: json_bool requires json=... path", sf.Name)
			}
			def.JSONPath = tag.jsonPath
		case FieldKindJSONList:
			if ft != FieldTypeString {
				return fmt.Errorf("field %s: json_list requires string elements", sf.Name)
			}
			if len(tag.jsonPath) == 0 {
				return fmt.Errorf("field %s: json_list requires json=... path", sf.Name)
			}
			def.JSONPath = tag.jsonPath
		}

		if tag.allowedOps != nil {
			def.AllowedComparisonOps = tag.allowedOps
		} else {
			def.AllowedComparisonOps = defaultAllowedComparisonOps(kind, ft)
		}

		if _, exists := fields[name]; exists {
			return fmt.Errorf("duplicate schema field name %q", name)
		}
		fields[name] = def

		celType, err := celTypeForField(def)
		if err != nil {
			return fmt.Errorf("field %s: %w", sf.Name, err)
		}
		*envOptions = append(*envOptions, cel.Variable(name, celType))
	}
	return nil
}

func inferFieldType(rt reflect.Type, kind FieldKind) (FieldType, error) {
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}

	switch kind {
	case FieldKindJSONList:
		if rt.Kind() != reflect.Slice && rt.Kind() != reflect.Array {
			return "", fmt.Errorf("json_list requires a slice/array field")
		}
		return inferFieldType(rt.Elem(), FieldKindScalar)
	}

	if rt == timeType {
		return FieldTypeTimestamp, nil
	}

	switch rt.Kind() {
	case reflect.String:
		return FieldTypeString, nil
	case reflect.Bool:
		return FieldTypeBool, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return FieldTypeInt, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return FieldTypeInt, nil
	default:
		return "", fmt.Errorf("unsupported Go type %s", rt.String())
	}
}

func celTypeForField(field *Field) (*cel.Type, error) {
	if field == nil {
		return nil, fmt.Errorf("field is nil")
	}
	// JSON list fields are exposed as a list, while everything else is a scalar.
	if field.Kind == FieldKindJSONList {
		elem, err := celScalarType(field.Type)
		if err != nil {
			return nil, err
		}
		return cel.ListType(elem), nil
	}
	return celScalarType(field.Type)
}

func celScalarType(ft FieldType) (*cel.Type, error) {
	switch ft {
	case FieldTypeString:
		return cel.StringType, nil
	case FieldTypeBool:
		return cel.BoolType, nil
	case FieldTypeInt, FieldTypeTimestamp:
		return cel.IntType, nil
	default:
		return nil, fmt.Errorf("unsupported field type %q", ft)
	}
}

func defaultAllowedComparisonOps(kind FieldKind, ft FieldType) map[ComparisonOperator]bool {
	switch kind {
	case FieldKindJSONList, FieldKindVirtualAlias:
		return map[ComparisonOperator]bool{}
	case FieldKindJSONBool:
		return map[ComparisonOperator]bool{
			CompareEq:  true,
			CompareNeq: true,
		}
	}

	switch ft {
	case FieldTypeBool:
		return map[ComparisonOperator]bool{
			CompareEq:  true,
			CompareNeq: true,
		}
	case FieldTypeString, FieldTypeInt, FieldTypeTimestamp:
		return map[ComparisonOperator]bool{
			CompareEq:  true,
			CompareNeq: true,
			CompareLt:  true,
			CompareLte: true,
			CompareGt:  true,
			CompareGte: true,
		}
	default:
		return nil
	}
}

func pickTagName(tag string) string {
	if tag == "" {
		return ""
	}
	name := strings.Split(tag, ",")[0]
	name = strings.TrimSpace(name)
	return name
}

func pickGormColumn(tag string) string {
	if tag == "" {
		return ""
	}
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "column:"):
			return strings.TrimPrefix(part, "column:")
		case strings.HasPrefix(part, "column="):
			return strings.TrimPrefix(part, "column=")
		}
	}
	return ""
}

func snakeCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s) + 4)

	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if (unicode.IsLower(prev) || unicode.IsDigit(prev)) || (next != 0 && unicode.IsLower(next)) {
					b.WriteByte('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
