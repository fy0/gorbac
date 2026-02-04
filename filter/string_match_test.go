package filter_test

import (
	"reflect"
	"testing"

	"github.com/fy0/gorbac/v3/filter"
	"github.com/google/cel-go/cel"
)

func stringMatchSchema() filter.Schema {
	return filter.Schema{
		Name: "string_match",
		Fields: map[string]*filter.Field{
			"name": {
				Name:             "name",
				Type:             filter.FieldTypeString,
				SupportsContains: true,
				Column:           filter.Column{Table: "t", Name: "name"},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("name", cel.StringType),
			cel.Variable("query", cel.StringType),
		},
	}
}

func TestStringContains_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(stringMatchSchema())
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
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`%foo%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`%foo%`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  "t.name ILIKE $1",
			wantArgs: []any{`%foo%`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`name.contains(query)`, filter.Bindings{
			"query": "foo",
		}, filter.RenderOptions{Dialect: tc.name})
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

func TestStringStartsWith_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(stringMatchSchema())
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
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`foo%`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`foo%`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  "t.name ILIKE $1",
			wantArgs: []any{`foo%`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`name.startsWith(query)`, filter.Bindings{
			"query": "foo",
		}, filter.RenderOptions{Dialect: tc.name})
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

func TestStringEndsWith_AllDialects(t *testing.T) {
	engine, err := filter.NewEngine(stringMatchSchema())
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
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`%foo`},
		},
		{
			name:     filter.DialectMySQL,
			wantSQL:  "`t`.`name` LIKE ?",
			wantArgs: []any{`%foo`},
		},
		{
			name:     filter.DialectPostgres,
			wantSQL:  "t.name ILIKE $1",
			wantArgs: []any{`%foo`},
		},
	}

	for _, tc := range tests {
		stmt, err := engine.CompileToStatement(`name.endsWith(query)`, filter.Bindings{
			"query": "foo",
		}, filter.RenderOptions{Dialect: tc.name})
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

func TestEvaluate_StringMatchFunctions(t *testing.T) {
	engine, err := filter.NewEngine(stringMatchSchema())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		expr string
		vars map[string]any
		want bool
	}{
		{
			expr: `name.contains(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "tool"},
			want: true,
		},
		{
			expr: `name.contains(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "zzz"},
			want: false,
		},
		{
			expr: `name.startsWith(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "infra"},
			want: true,
		},
		{
			expr: `name.startsWith(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "tool"},
			want: false,
		},
		{
			expr: `name.endsWith(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "toolkit"},
			want: true,
		},
		{
			expr: `name.endsWith(query)`,
			vars: map[string]any{"name": "infra toolkit", "query": "infra"},
			want: false,
		},
	}

	for _, tc := range tests {
		prog, err := engine.Compile(tc.expr)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.expr, err)
		}
		ok, err := prog.IsGranted(tc.vars, filter.EvalOptions{})
		if err != nil {
			t.Fatalf("eval %q: %v", tc.expr, err)
		}
		if ok != tc.want {
			t.Fatalf("expr %q: want %v got %v", tc.expr, tc.want, ok)
		}
	}
}
