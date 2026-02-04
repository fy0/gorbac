package gorbac_test

import (
	"testing"

	"github.com/fy0/gorbac/v3"
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

func TestNewFilterProgram_WithMacro(t *testing.T) {
	rbac := gorbac.New[string]()

	role1 := gorbac.NewRole("r1")
	_ = role1.Assign(gorbac.NewFilterPermission("read", `selfUser()`))
	_ = rbac.Add(role1)

	role2 := gorbac.NewRole("r2")
	_ = role2.Assign(gorbac.NewFilterPermission("read", `visibility == "PUBLIC"`))
	_ = rbac.Add(role2)

	selfUser := cel.GlobalMacro("selfUser", 0, func(eh cel.MacroExprFactory, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		return eh.NewCall(operators.Equals, eh.NewIdent("creator_id"), eh.NewIdent("current_user_id")), nil
	})

	program, err := gorbac.NewFilterProgram(
		rbac,
		[]string{"r1", "r2"},
		[]gorbac.Permission[string]{gorbac.NewPermission("read")},
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
