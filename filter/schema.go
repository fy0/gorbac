package filter

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// DialectName enumerates supported SQL dialects.
type DialectName string

const (
	DialectSQLite   DialectName = "sqlite"
	DialectMySQL    DialectName = "mysql"
	DialectPostgres DialectName = "postgres"
	// DialectPostgresNamedArgs renders Postgres SQL using named arguments (`@name`).
	//
	// The generated statement uses `Statement.NamedArgs` instead of positional `Statement.Args`.
	DialectPostgresNamedArgs DialectName = "postgres_pgx"
)

// FieldType represents the logical type of a field.
type FieldType string

const (
	FieldTypeString    FieldType = "string"
	FieldTypeInt       FieldType = "int"
	FieldTypeBool      FieldType = "bool"
	FieldTypeTimestamp FieldType = "timestamp"
)

// FieldKind describes how a field is stored.
type FieldKind string

const (
	FieldKindScalar       FieldKind = "scalar"
	FieldKindBoolColumn   FieldKind = "bool_column"
	FieldKindJSONBool     FieldKind = "json_bool"
	FieldKindJSONList     FieldKind = "json_list"
	FieldKindVirtualAlias FieldKind = "virtual_alias"
)

// Column identifies the backing table column.
type Column struct {
	Table string
	Name  string
}

// Field captures the schema metadata for an exposed CEL identifier.
type Field struct {
	Name                 string
	Kind                 FieldKind
	Type                 FieldType
	Column               Column
	JSONPath             []string
	AliasFor             string
	SupportsContains     bool
	Expressions          map[DialectName]string
	AllowedComparisonOps map[ComparisonOperator]bool
}

// Schema collects CEL environment options and field metadata.
type Schema struct {
	Name       string
	Fields     map[string]*Field
	EnvOptions []cel.EnvOption
}

// Field returns the field metadata if present.
func (s Schema) Field(name string) (*Field, bool) {
	f, ok := s.Fields[name]
	if !ok || f == nil {
		return nil, false
	}
	return f, ok
}

// ResolveAlias resolves a virtual alias to its target field.
func (s Schema) ResolveAlias(name string) (*Field, bool) {
	field, ok := s.Fields[name]
	if !ok || field == nil {
		return nil, false
	}
	if field.Kind == FieldKindVirtualAlias {
		target, ok := s.Fields[field.AliasFor]
		if !ok || target == nil {
			return nil, false
		}
		return target, true
	}
	return field, true
}

// NowFunction exposes a CEL `now()` helper, returning unix seconds.
var NowFunction = cel.Function("now",
	cel.Overload("now",
		[]*cel.Type{},
		cel.IntType,
		cel.FunctionBinding(func(_ ...ref.Val) ref.Val {
			return types.Int(time.Now().Unix())
		}),
	),
)

// columnExpr returns the field expression for the given dialect, applying
// any schema-specific overrides (e.g. UNIX timestamp conversions).
func (f Field) columnExpr(d DialectName) string {
	base := qualifyColumn(d, f.Column)
	if expr, ok := f.Expressions[d]; ok && expr != "" {
		return fmt.Sprintf(expr, base)
	}
	return base
}
