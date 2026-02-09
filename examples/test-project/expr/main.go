package main

import (
	"fmt"
	"log"
	"maps"
	"strings"

	"github.com/fy0/gorbac/v3"
	"github.com/fy0/gorbac/v3/filter"
	"github.com/google/cel-go/cel"
)

// This example shows a "native expr style" workflow:
//
//  1. Permissions directly carry CEL expressions (no scope conversion)
//  2. Use gorbac helpers to compile into a single SQL WHERE fragment
//  3. Optionally AND with a user query expression
func main() {
	type projectRow struct {
		ProjectID  int64  `json:"project_id" db:"id"`
		CreatorID  int64  `json:"creator_id"`
		Visibility string `json:"visibility"`
		Name       string `json:"name" filter:",contains"`

		// Unsupported types should be ignored unless explicitly tagged.
		Extra []string `json:"extra"`

		unexported string
	}

	schema := filter.Schema{
		Name: "test_project_expr",
		Fields: map[string]*filter.Field{
			"creator_id": {
				Name: "creator_id",
				Type: filter.FieldTypeInt,
				Column: filter.Column{
					Table: "project",
					Name:  "creator_id",
				},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{filter.CompareEq: true},
			},
			"visibility": {
				Name: "visibility",
				Type: filter.FieldTypeString,
				Column: filter.Column{
					Table: "project",
					Name:  "visibility",
				},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{filter.CompareEq: true},
			},
			"name": {
				Name:             "name",
				Type:             filter.FieldTypeString,
				SupportsContains: true,
				Column: filter.Column{
					Table: "project",
					Name:  "name",
				},
			},
		},
		EnvOptions: []cel.EnvOption{
			// row fields
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("visibility", cel.StringType),
			cel.Variable("name", cel.StringType),
			// request params (bindings)
			cel.Variable("current_user_id", cel.IntType),
			cel.Variable("query", cel.StringType),
		},
	}

	s1, _ := filter.SchemaFromStruct("test_project_expr", "project", &projectRow{})
	s1.EnvOptions = schema.EnvOptions
	schema = s1

	rbac := gorbac.New[string]()

	roleCreator := gorbac.NewRole("role-creator")
	_ = roleCreator.Assign(gorbac.NewFilterPermission("project.read", `creator_id == current_user_id`))
	_ = rbac.Add(roleCreator)

	rolePublic := gorbac.NewRole("role-public")
	_ = rolePublic.Assign(gorbac.NewFilterPermission("project.read", `visibility == "PUBLIC"`))
	_ = rolePublic.Assign(gorbac.NewPermission("project.query"))
	_ = rbac.Add(rolePublic)

	userRoles := []string{"role-creator", "role-public"}
	bindings := filter.Bindings{
		"current_user_id": int64(1001),
		"query":           "infra",
	}

	required := []gorbac.Permission[string]{gorbac.NewPermission("project.read")}

	// 1) data scope from permissions (OR across roles) + optional user query (AND).
	queryExpr := `query == "" || name.startsWith(query)`
	scopeProgram, err := gorbac.NewFilterProgram(rbac, userRoles, required, schema, gorbac.WithExtraFilterCEL(queryExpr))
	if err != nil {
		log.Fatal(err)
	}

	scopeStmt, err := scopeProgram.RenderSQL(bindings, filter.RenderOptions{Dialect: filter.DialectPostgres})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("SQL:  %s\nARGS: %#v\n", scopeStmt.SQL, scopeStmt.Args)

	// 2) In-memory check (no SQL): compile and evaluate against a concrete row.
	//
	// This is useful for:
	//   - unit tests
	//   - authorization checks on in-memory objects
	//   - non-SQL backends
	fmt.Println("\nIn-memory evaluation:")
	samples := []struct {
		CreatorID  int64
		Visibility string
		Name       string
	}{
		{CreatorID: 1001, Visibility: "PRIVATE", Name: "infra"},  // matches creator scope
		{CreatorID: 2002, Visibility: "PUBLIC", Name: "secret"},  // matches public scope
		{CreatorID: 2002, Visibility: "PRIVATE", Name: "secret"}, // matches none
	}
	for _, row := range samples {
		vars := map[string]any{
			"creator_id": row.CreatorID,
			"visibility": row.Visibility,
			"name":       row.Name,
		}
		maps.Copy(vars, bindings)

		allowed, err := scopeProgram.IsGranted(vars, filter.EvalOptions{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("creator_id=%d visibility=%s name=%s -> allowed=%v\n", row.CreatorID, row.Visibility, row.Name, allowed)
	}
	fmt.Println()
}

func joinWhere(parts []string) string {
	if len(parts) == 0 {
		return "TRUE"
	}
	return strings.Join(parts, " AND ")
}
