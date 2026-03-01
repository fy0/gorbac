package gorbac

import (
	"context"
	"testing"

	"github.com/fy0/gorbac/v3/filter"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
)

func testFilterSchema() filter.Schema {
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
					filter.CompareEq: true,
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
					filter.CompareEq: true,
				},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("visibility", cel.StringType),
			cel.Variable("current_user_id", cel.IntType),
		},
	}
}

func buildTestProgram(
	rbac RBAC[string],
	roles []string,
	requiredFilterPermissions []Permission[string],
	schema filter.Schema,
	opts ...filter.EngineOption,
) (*filter.Program, error) {
	exprs, err := FilterExprsForRoles(context.Background(), rbac, roles, requiredFilterPermissions)
	if err != nil {
		return nil, err
	}
	return NewFilterProgramFromCEL(schema, exprs, opts...)
}

func TestNewFilterProgram_WithMacro(t *testing.T) {
	ctx := context.Background()
	rbac := New[string]()

	role1 := NewRole("r1")
	_ = role1.Assign(ctx, NewFilterPermission("read", `selfUser()`))
	_ = rbac.Add(ctx, role1)

	role2 := NewRole("r2")
	_ = role2.Assign(ctx, NewFilterPermission("read", `visibility == "PUBLIC"`))
	_ = rbac.Add(ctx, role2)

	selfUser := cel.GlobalMacro("selfUser", 0, func(eh cel.MacroExprFactory, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		return eh.NewCall(operators.Equals, eh.NewIdent("creator_id"), eh.NewIdent("current_user_id")), nil
	})

	program, err := buildTestProgram(
		rbac,
		[]string{"r1", "r2"},
		[]Permission[string]{NewPermission("read")},
		testFilterSchema(),
		filter.WithMacros(selfUser),
	)
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := program.RenderSQL(filter.Bindings{"current_user_id": int64(1)}, filter.RenderOptions{Dialect: filter.DialectPostgres})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.creator_id = $1 OR t.visibility = $2)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 2 || stmt.Args[0] != int64(1) || stmt.Args[1] != "PUBLIC" {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
}

func TestNewFilterProgram_WithExtraFilterCEL_StdPermission(t *testing.T) {
	ctx := context.Background()
	rbac := New[string]()

	role := NewRole("r1")
	_ = role.Assign(ctx, NewPermission("read"))
	_ = rbac.Add(ctx, role)

	program, err := buildTestProgram(
		rbac,
		[]string{"r1"},
		[]Permission[string]{NewPermission("read")},
		testFilterSchema(),
		filter.WithExtraFilterCEL(`visibility == "PUBLIC"`),
	)
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := program.RenderSQL(nil, filter.RenderOptions{Dialect: filter.DialectPostgres})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `t.visibility = $1`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 1 || stmt.Args[0] != "PUBLIC" {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}

	allowed, err := program.IsCondGranted(map[string]any{"visibility": "PUBLIC"}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatalf("expected PUBLIC to be allowed")
	}

	allowed, err = program.IsCondGranted(map[string]any{"visibility": "PRIVATE"}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatalf("expected PRIVATE to be denied")
	}
}

func TestFilterExprsForRoles(t *testing.T) {
	ctx := context.Background()
	rbac := New[string]()

	role1 := NewRole("r1")
	_ = role1.Assign(ctx, NewFilterPermission("read", `creator_id == current_user_id`))
	_ = rbac.Add(ctx, role1)

	role2 := NewRole("r2")
	_ = role2.Assign(ctx, NewPermission("read"))
	_ = rbac.Add(ctx, role2)

	exprs, err := FilterExprsForRoles(
		ctx,
		rbac,
		[]string{"r1", "r2"},
		[]Permission[string]{NewPermission("read")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(exprs) != 2 {
		t.Fatalf("expected 2 exprs, got %d", len(exprs))
	}
	wantExpr := `(creator_id == current_user_id)`
	if exprs[0] != wantExpr {
		t.Fatalf("unexpected expr.\nwant: %s\ngot:  %s", wantExpr, exprs[0])
	}
	if exprs[1] != "true" {
		t.Fatalf("unexpected expr.\nwant: %s\ngot:  %s", "true", exprs[1])
	}
}

func TestNewFilterProgramFromCEL(t *testing.T) {
	exprs := []string{
		`creator_id == current_user_id`,
		`visibility == "PUBLIC"`,
	}

	program, err := NewFilterProgramFromCEL(testFilterSchema(), exprs)
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := program.RenderSQL(filter.Bindings{"current_user_id": int64(1)}, filter.RenderOptions{Dialect: filter.DialectPostgres})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.creator_id = $1 OR t.visibility = $2)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	if len(stmt.Args) != 2 || stmt.Args[0] != int64(1) || stmt.Args[1] != "PUBLIC" {
		t.Fatalf("unexpected args: %#v", stmt.Args)
	}
}
