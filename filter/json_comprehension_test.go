package filter_test

import (
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/mikespook/gorbac/v3/filter"
)

func jsonSchema() filter.Schema {
	return filter.Schema{
		Name: "json",
		Fields: map[string]*filter.Field{
			"tags": {
				Name:     "tags",
				Kind:     filter.FieldKindJSONList,
				Type:     filter.FieldTypeString,
				Column:   filter.Column{Table: "t", Name: "payload"},
				JSONPath: []string{"tags"},
			},
			"tag": {
				Name:     "tag",
				Kind:     filter.FieldKindVirtualAlias,
				Type:     filter.FieldTypeString,
				AliasFor: "tags",
			},
			"has_task_list": {
				Name:     "has_task_list",
				Kind:     filter.FieldKindJSONBool,
				Type:     filter.FieldTypeBool,
				Column:   filter.Column{Table: "t", Name: "payload"},
				JSONPath: []string{"property", "hasTaskList"},
				AllowedComparisonOps: map[filter.ComparisonOperator]bool{
					filter.CompareEq:  true,
					filter.CompareNeq: true,
				},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("tags", cel.ListType(cel.StringType)),
			cel.Variable("tag", cel.StringType),
			cel.Variable("has_task_list", cel.BoolType),
			cel.Variable("q", cel.StringType),
		},
	}
}

func TestJSONBoolPredicate_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    filter.DialectName
		wantSQL string
	}{
		{
			name:    filter.DialectSQLite,
			wantSQL: "JSON_EXTRACT(`t`.`payload`, '$.property.hasTaskList') IS TRUE",
		},
		{
			name:    filter.DialectMySQL,
			wantSQL: "COALESCE(JSON_EXTRACT(`t`.`payload`, '$.property.hasTaskList'), CAST('false' AS JSON)) = CAST('true' AS JSON)",
		},
		{
			name:    filter.DialectPostgres,
			wantSQL: "(t.payload->'property'->>'hasTaskList')::boolean IS TRUE",
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`has_task_list`, nil, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if len(stmt.Args) != 0 {
			t.Fatalf("dialect %s: expected no args, got %#v", tc.name, stmt.Args)
		}
	}
}

func TestJSONListElementIn_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     filter.DialectName
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     filter.DialectSQLite,
			wantSQL:  "JSON_EXTRACT(`t`.`payload`, '$.tags') LIKE ?",
			wantArgs: []any{`%"foo"%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  "JSON_CONTAINS(JSON_EXTRACT(`t`.`payload`, '$.tags'), ?)",
			wantArgs: []any{`"foo"`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  "t.payload->'tags' @> jsonb_build_array($1::json)",
			wantArgs: []any{`"foo"`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`"foo" in tags`, nil, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if !reflect.DeepEqual(stmt.Args, tc.wantArgs) {
			t.Fatalf("dialect %s: unexpected args.\nwant: %#v\ngot:  %#v", tc.name, tc.wantArgs, stmt.Args)
		}
	}
}

func TestJSONListSizeComparison_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    filter.DialectName
		wantSQL string
	}{
		{
			name:    filter.DialectSQLite,
			wantSQL: "JSON_ARRAY_LENGTH(COALESCE(JSON_EXTRACT(`t`.`payload`, '$.tags'), JSON_ARRAY())) > ?",
		},
		{
			name:    filter.DialectMySQL,
			wantSQL: "JSON_LENGTH(COALESCE(JSON_EXTRACT(`t`.`payload`, '$.tags'), JSON_ARRAY())) > ?",
		},
		{
			name:    filter.DialectPostgres,
			wantSQL: "jsonb_array_length(COALESCE(t.payload->'tags', '[]'::jsonb)) > $1",
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`size(tags) > 0`, nil, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if !reflect.DeepEqual(stmt.Args, []any{int64(0)}) {
			t.Fatalf("dialect %s: unexpected args: %#v", tc.name, stmt.Args)
		}
	}
}

func TestTagAliasInList_Postgres(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`tag in ["foo"]`, nil, filter.RenderOptions{
		Dialect: filter.DialectPostgres,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(t.payload->'tags' @> jsonb_build_array($1::json) OR (t.payload->'tags')::text LIKE $2)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
	wantArgs := []any{`"foo"`, `%"foo/%`}
	if !reflect.DeepEqual(stmt.Args, wantArgs) {
		t.Fatalf("unexpected args.\nwant: %#v\ngot:  %#v", wantArgs, stmt.Args)
	}
}

func TestComprehensionContains_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     filter.DialectName
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     filter.DialectSQLite,
			wantSQL:  `(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') != '[]')`,
			wantArgs: []any{`%foo%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  `(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_LENGTH(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags')) > 0)`,
			wantArgs: []any{`%foo%`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  `((t.payload->'tags')::text LIKE $1 AND t.payload->'tags' IS NOT NULL AND jsonb_array_length(t.payload->'tags') > 0)`,
			wantArgs: []any{`%foo%`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`tags.exists(t, t.contains(q))`, filter.Bindings{
			"q": "foo",
		}, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if !reflect.DeepEqual(stmt.Args, tc.wantArgs) {
			t.Fatalf("dialect %s: unexpected args.\nwant: %#v\ngot:  %#v", tc.name, tc.wantArgs, stmt.Args)
		}
	}
}

func TestComprehensionStartsWith_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     filter.DialectName
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     filter.DialectSQLite,
			wantSQL:  `((JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? OR JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ?) AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') != '[]')`,
			wantArgs: []any{`%"foo"%`, `%"foo%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  `((JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? OR JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ?) AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_LENGTH(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags')) > 0)`,
			wantArgs: []any{`%"foo"%`, `%"foo%`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  `((t.payload->'tags' @> jsonb_build_array($1::json) OR (t.payload->'tags')::text LIKE $2) AND t.payload->'tags' IS NOT NULL AND jsonb_array_length(t.payload->'tags') > 0)`,
			wantArgs: []any{`"foo"`, `%"foo%`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`tags.exists(t, t.startsWith(q))`, filter.Bindings{
			"q": "foo",
		}, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if !reflect.DeepEqual(stmt.Args, tc.wantArgs) {
			t.Fatalf("dialect %s: unexpected args.\nwant: %#v\ngot:  %#v", tc.name, tc.wantArgs, stmt.Args)
		}
	}
}

func TestComprehensionEndsWith_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     filter.DialectName
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     filter.DialectSQLite,
			wantSQL:  `(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') != '[]')`,
			wantArgs: []any{`%foo"%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  `(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') LIKE ? AND JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags') IS NOT NULL AND JSON_LENGTH(JSON_EXTRACT(` + "`t`" + `.` + "`payload`" + `, '$.tags')) > 0)`,
			wantArgs: []any{`%foo"%`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  `((t.payload->'tags')::text LIKE $1 AND t.payload->'tags' IS NOT NULL AND jsonb_array_length(t.payload->'tags') > 0)`,
			wantArgs: []any{`%foo"%`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`tags.exists(t, t.endsWith(q))`, filter.Bindings{
			"q": "foo",
		}, filter.RenderOptions{
			Dialect: tc.name,
		})
		if err != nil {
			t.Fatalf("dialect %s: %v", tc.name, err)
		}
		if stmt.SQL != tc.wantSQL {
			t.Fatalf("dialect %s: unexpected SQL.\nwant: %s\ngot:  %s", tc.name, tc.wantSQL, stmt.SQL)
		}
		if !reflect.DeepEqual(stmt.Args, tc.wantArgs) {
			t.Fatalf("dialect %s: unexpected args.\nwant: %#v\ngot:  %#v", tc.name, tc.wantArgs, stmt.Args)
		}
	}
}

func TestEvaluate_ElementIn(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	prog, err := engine.Compile(`"foo" in tags`)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := prog.IsGranted(map[string]any{
		"tags": []string{"bar", "foo"},
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected element membership to pass")
	}

	ok, err = prog.IsGranted(map[string]any{
		"tags": []string{"bar"},
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected element membership to fail")
	}
}

func TestEvaluate_ComprehensionExists(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	prog, err := engine.Compile(`tags.exists(t, t.contains(q))`)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "pha",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected comprehension to pass")
	}

	ok, err = prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "zzz",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected comprehension to fail")
	}
}

func TestEvaluate_ComprehensionStartsWith(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	prog, err := engine.Compile(`tags.exists(t, t.startsWith(q))`)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "al",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected startsWith comprehension to pass")
	}

	ok, err = prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "pha",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected startsWith comprehension to fail")
	}
}

func TestEvaluate_ComprehensionEndsWith(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	prog, err := engine.Compile(`tags.exists(t, t.endsWith(q))`)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "ha",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected endsWith comprehension to pass")
	}

	ok, err = prog.IsGranted(map[string]any{
		"tags": []string{"alpha", "bravo"},
		"q":    "al",
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected endsWith comprehension to fail")
	}
}

func TestEvaluate_TagAliasInList(t *testing.T) {
	engine, err := filter.NewEngine(jsonSchema())
	if err != nil {
		t.Fatal(err)
	}

	prog, err := engine.Compile(`tag in ["foo"]`)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := prog.IsGranted(map[string]any{
		"tags": []string{"foo/bar"},
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected hierarchical tag alias to match foo/bar")
	}

	ok, err = prog.IsGranted(map[string]any{
		"tags": []string{"foobar"},
	}, filter.EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected hierarchical tag alias not to match foobar")
	}
}
