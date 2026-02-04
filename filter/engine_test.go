package filter_test

import (
	"testing"

	"github.com/fy0/gorbac/v3/filter"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
)

func testSchema() filter.Schema {
	return filter.Schema{
		Name: "test",
		Fields: map[string]*filter.Field{
			"creator_id": {
				Name: "creator_id",
				Type: filter.FieldTypeInt,
				Column: filter.Column{
					Table: "t",
					Name:  "creator_id",
				},
				Expressions: map[filter.DialectName]string{},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{
					filter.CompareEq:  true,
					filter.CompareNeq: true,
				},
			},
			"visibility": {
				Name: "visibility",
				Type: filter.FieldTypeString,
				Column: filter.Column{
					Table: "t",
					Name:  "visibility",
				},
				Expressions: map[filter.DialectName]string{},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{
					filter.CompareEq:  true,
					filter.CompareNeq: true,
				},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("visibility", cel.StringType),
		},
	}
}

func TestEngineCompileToStatement_Postgres(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`creator_id == 123 && visibility in ["PUBLIC","PROTECTED"]`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.creator_id = $1 AND t.visibility IN ($2,$3))`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 3 {
		t.Fatalf("unexpected args length: %d", len(stmt.Args))
	}
	if stmt.Args[0] != int64(123) {
		t.Fatalf("unexpected arg[0]: %#v", stmt.Args[0])
	}
	if stmt.Args[1] != "PUBLIC" || stmt.Args[2] != "PROTECTED" {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
}

func TestEngineFlattensLogicalChains_Postgres(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`creator_id == 1 || creator_id == 2 || creator_id == 3`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.creator_id = $1 OR t.creator_id = $2 OR t.creator_id = $3)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 3 || stmt.Args[0] != int64(1) || stmt.Args[1] != int64(2) || stmt.Args[2] != int64(3) {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}

	stmt, err = engine.CompileToStatement(`creator_id == 1 && visibility == "PUBLIC" && visibility != "PRIVATE"`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL = `(t.creator_id = $1 AND t.visibility = $2 AND t.visibility != $3)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 3 || stmt.Args[0] != int64(1) || stmt.Args[1] != "PUBLIC" || stmt.Args[2] != "PRIVATE" {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
}

func TestEngineTrivialClearsArgs(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`true || creator_id == 1`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "" {
		t.Fatalf("expected trivial SQL, got %q", stmt.SQL)
	}
	if len(stmt.Args) != 0 {
		t.Fatalf("expected no args for trivial statement, got %#v", stmt.Args)
	}
}

func TestEngineMacrosAndCompileHook(t *testing.T) {
	schema := testSchema()
	schema.EnvOptions = append(schema.EnvOptions, cel.Variable("current_user_id", cel.IntType))

	selfUser := cel.GlobalMacro("selfUser", 0, func(eh cel.MacroExprFactory, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		return eh.NewCall(operators.Equals, eh.NewIdent("creator_id"), eh.NewIdent("current_user_id")), nil
	})

	type ctxKey struct{}
	// ctx := context.WithValue(context.Background(), ctxKey{}, "ok")
	_ = ctxKey{}

	hookCalled := false
	hook := func(schema filter.Schema, filterExpr string, ast *cel.Ast, cond filter.Condition) (filter.Condition, error) {
		hookCalled = true
		// 补充: ctx作用有限，不带入了
		// if ctx.Value(ctxKey{}) != "ok" {
		// 	return nil, fmt.Errorf("compile hook context was not propagated")
		// }

		// Drop the right side of an `a && b` filter.
		if lc, ok := cond.(*filter.LogicalCondition); ok && lc.Operator == filter.LogicalAnd {
			return lc.Left, nil
		}
		return nil, nil
	}

	engine, err := filter.NewEngine(schema, filter.WithMacros(selfUser), filter.WithCompileHook(hook))
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`selfUser() && visibility == "PUBLIC"`, filter.Bindings{
		"current_user_id": int64(123),
	}, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatalf("expected compile hook to be called")
	}

	// The compile hook should have dropped the visibility predicate.
	wantSQL := `t.creator_id = $1`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 1 || stmt.Args[0] != int64(123) {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
}
