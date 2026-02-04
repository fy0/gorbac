package filter

import "github.com/google/cel-go/cel"

// DialectSQL stores dialect-specific SQL templates.
//
// Templates are SQL fragments used in a WHERE clause and must evaluate to a
// boolean expression in the target dialect.
//
// Placeholders:
//   - `{{field_name}}` is replaced with the schema column expression for that field.
//   - `?` are replaced with dialect placeholders and populated from condition args.
type DialectSQL struct {
	Default  string
	SQLite   string
	MySQL    string
	Postgres string
}

func (s DialectSQL) template(d DialectName) string {
	switch d {
	case DialectSQLite:
		if s.SQLite != "" {
			return s.SQLite
		}
	case DialectMySQL:
		if s.MySQL != "" {
			return s.MySQL
		}
	case DialectPostgres:
		if s.Postgres != "" {
			return s.Postgres
		}
	}
	return s.Default
}

// SQLPredicateEval evaluates a custom predicate in-memory.
//
// The args slice contains resolved argument values (literals / param values).
// vars contains the full evaluation var map (schema fields and params).
//
// Returning an error will fail evaluation.
type SQLPredicateEval func(schema Schema, vars map[string]any, args []any, opts EvalOptions) (bool, error)

// SQLPredicate defines a custom predicate which renders to dialect-specific SQL.
type SQLPredicate struct {
	SQL  DialectSQL
	Eval SQLPredicateEval
}

// SQLFunction declares the CEL function used to reference registered SQL predicates:
//
//   - sql("predicate")
//   - sql("predicate", [arg1, arg2, ...])
//
// The function is only used for parsing and type-checking; runtime CEL evaluation
// is not used by this filter engine.
var SQLFunction = cel.Function("sql",
	cel.Overload("sql_string", []*cel.Type{cel.StringType}, cel.BoolType),
	cel.Overload("sql_string_list", []*cel.Type{cel.StringType, cel.ListType(cel.DynType)}, cel.BoolType),
)

// WithSQLPredicate registers a custom SQL predicate.
//
// The predicate is referenced from CEL via `sql("<name>")` or `sql("<name>", [...])`.
func WithSQLPredicate(name string, pred SQLPredicate) EngineOption {
	return func(cfg *engineConfig) {
		if name == "" {
			return
		}
		if cfg.sqlPredicates == nil {
			cfg.sqlPredicates = make(map[string]SQLPredicate, 4)
		}
		cfg.sqlPredicates[name] = pred
	}
}
