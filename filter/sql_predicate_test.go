package filter_test

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/mikespook/gorbac/v3/filter"
)

func TestSQLPredicateCondition_AllDialects(t *testing.T) {
	schema := filter.Schema{
		Name: "sql_predicate",
		Fields: map[string]*filter.Field{
			"creator_id": {
				Name: "creator_id",
				Type: filter.FieldTypeInt,
				Column: filter.Column{
					Table: "t",
					Name:  "creator_id",
				},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{filter.CompareEq: true},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("current_user_id", cel.IntType),
		},
	}

	engine, err := filter.NewEngine(schema, filter.WithSQLPredicate("is_creator", filter.SQLPredicate{
		SQL: filter.DialectSQL{
			Default: "EXISTS (SELECT 1 WHERE {{creator_id}} = ?)",
		},
		Eval: func(schema filter.Schema, vars map[string]any, args []any, opts filter.EvalOptions) (bool, error) {
			if len(args) != 1 {
				return false, fmt.Errorf("expected one arg, got %d", len(args))
			}
			creator, ok := vars["creator_id"].(int64)
			if !ok {
				return false, fmt.Errorf("creator_id must be int64, got %T", vars["creator_id"])
			}
			current, ok := args[0].(int64)
			if !ok {
				return false, fmt.Errorf("arg must be int64, got %T", args[0])
			}
			return creator == current, nil
		},
	}))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		dialect  filter.DialectName
		wantSQL  string
		wantArgs []any
	}{
		{
			dialect:  filter.DialectSQLite,
			wantSQL:  "EXISTS (SELECT 1 WHERE `t`.`creator_id` = ?)",
			wantArgs: []any{int64(123)},
		},
		{
			dialect:  filter.DialectMySQL,
			wantSQL:  "EXISTS (SELECT 1 WHERE `t`.`creator_id` = ?)",
			wantArgs: []any{int64(123)},
		},
		{
			dialect:  filter.DialectPostgres,
			wantSQL:  "EXISTS (SELECT 1 WHERE t.creator_id = $1)",
			wantArgs: []any{int64(123)},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`sql("is_creator", [current_user_id])`, filter.Bindings{
			"current_user_id": int64(123),
		}, filter.RenderOptions{
			Dialect: tc.dialect,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.dialect, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.dialect, tc.wantSQL, stmt.SQL)
		}
		if len(stmt.Args) != len(tc.wantArgs) {
			t.Fatalf("dialect %s: unexpected args: %#v", tc.dialect, stmt.Args)
		}
		for i := range stmt.Args {
			if stmt.Args[i] != tc.wantArgs[i] {
				t.Fatalf("dialect %s: unexpected args: %#v", tc.dialect, stmt.Args)
			}
		}
	}

	prog, err := engine.Compile(`sql("is_creator", [current_user_id])`)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := prog.IsGranted(map[string]any{
		"creator_id":      int64(123),
		"current_user_id": int64(123),
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected predicate eval to pass")
	}
}

func TestSQLPredicateCondition_PlaceholderCountMismatch(t *testing.T) {
	schema := filter.Schema{
		Name:   "sql_predicate",
		Fields: map[string]*filter.Field{},
		EnvOptions: []cel.EnvOption{
			cel.Variable("current_user_id", cel.IntType),
		},
	}

	engine, err := filter.NewEngine(schema, filter.WithSQLPredicate("needs_arg", filter.SQLPredicate{
		SQL: filter.DialectSQL{Default: "1 = ?"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.CompileToStatement(`sql("needs_arg")`, filter.Bindings{}, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err == nil {
		t.Fatalf("expected error due to placeholder mismatch")
	}
}

func TestSQLPredicateCondition_SubqueryPlaceholderNumbering_Postgres_CreatorOrMember(t *testing.T) {
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
		filter.RenderOptions{Dialect: filter.DialectPostgres},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Run with `go test -run TestSQLPredicateCondition_SubqueryPlaceholderNumbering_Postgres -v`
	// to see the actual SQL+Args.
	t.Logf("SQL:  %s", stmt.SQL)
	t.Logf("ARGS: %#v", stmt.Args)

	wantSQL := `(p.creator_id = $1 OR EXISTS (SELECT 1 FROM project_member pm WHERE pm.project_id = p.id AND pm.user_id = $2::bigint AND pm.status = 'ACTIVE'))`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	wantArgs := []any{int64(1001), int64(1001)}
	if len(stmt.Args) != len(wantArgs) {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
	for i := range wantArgs {
		if stmt.Args[i] != wantArgs[i] {
			t.Fatalf("unexpected args: %#v", stmt.Args)
		}
	}
}
