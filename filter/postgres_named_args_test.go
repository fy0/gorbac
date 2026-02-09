// Tests for Postgres named-args dialect (`@p1` placeholders).
package filter_test

import (
	"reflect"
	"testing"

	"github.com/fy0/gorbac/v3/filter"
	"github.com/google/cel-go/cel"
)

func TestEngineCompileToStatement_PostgresNamedArgs(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`creator_id == 123 && visibility in ["PUBLIC","PROTECTED"]`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgresNamedArgs,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.creator_id = @p1 AND t.visibility = ANY(@p2))`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 0 {
		t.Fatalf("expected positional args to be empty, got %#v", stmt.Args)
	}
	wantNamed := filter.Bindings{
		"p1": int64(123),
		"p2": []string{"PUBLIC", "PROTECTED"},
	}
	if !reflect.DeepEqual(stmt.NamedArgs, wantNamed) {
		t.Fatalf("unexpected named args.\nwant: %#v\ngot:  %#v", wantNamed, stmt.NamedArgs)
	}
}

func TestSQLPredicateCondition_SubqueryPlaceholderNumbering_PostgresNamedArgs_CreatorOrMember(t *testing.T) {
	schema := filter.Schema{
		Name: "sql_predicate_subquery",
		Fields: map[string]*filter.Field{
			"project_id": {
				Name: "project_id",
				Kind: filter.FieldKindScalar,
				Type: filter.FieldTypeInt,
				Column: filter.Column{
					Table: "p",
					Name:  "id",
				},
			},
			"creator_id": {
				Name: "creator_id",
				Kind: filter.FieldKindScalar,
				Type: filter.FieldTypeInt,
				Column: filter.Column{
					Table: "p",
					Name:  "creator_id",
				},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{filter.CompareEq: true},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("project_id", cel.IntType),
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("current_user_id", cel.IntType),
		},
	}

	engine, err := filter.NewEngine(schema, filter.WithSQLPredicate("project_member", filter.SQLPredicate{
		SQL: filter.DialectSQL{
			Postgres: "EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = {{project_id}} AND pm.user_id = ?::bigint AND pm.status = 'ACTIVE')",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(
		`creator_id == current_user_id || sql("project_member", [current_user_id])`,
		filter.Bindings{
			"current_user_id": int64(1001),
		},
		filter.RenderOptions{Dialect: filter.DialectPostgresNamedArgs},
	)
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(p.creator_id = @p1 OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.user_id = @p2::bigint AND pm.status = 'ACTIVE'))`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 0 {
		t.Fatalf("expected positional args to be empty, got %#v", stmt.Args)
	}
	wantNamed := filter.Bindings{
		"p1": int64(1001),
		"p2": int64(1001),
	}
	if !reflect.DeepEqual(stmt.NamedArgs, wantNamed) {
		t.Fatalf("unexpected named args.\nwant: %#v\ngot:  %#v", wantNamed, stmt.NamedArgs)
	}
}
