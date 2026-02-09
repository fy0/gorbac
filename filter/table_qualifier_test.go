package filter_test

import (
	"testing"

	"github.com/fy0/gorbac/v3/filter"
)

func TestRenderOptions_TableAliases_Postgres(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`creator_id == 1 && visibility == "PUBLIC"`, nil, filter.RenderOptions{
		Dialect:      filter.DialectPostgres,
		TableAliases: map[string]string{"t": "p"},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(p.creator_id = $1 AND p.visibility = $2)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
}

func TestRenderOptions_OmitTableQualifier_Postgres(t *testing.T) {
	engine, err := filter.NewEngine(testSchema())
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := engine.CompileToStatement(`creator_id == 1 && visibility == "PUBLIC"`, nil, filter.RenderOptions{
		Dialect:             filter.DialectPostgres,
		OmitTableQualifier: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSQL := `(creator_id = $1 AND visibility = $2)`
	if stmt.SQL != wantSQL {
		t.Fatalf("unexpected SQL.\nwant: %s\ngot:  %s", wantSQL, stmt.SQL)
	}
}

