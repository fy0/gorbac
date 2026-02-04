package filter_test

import (
	"testing"

	"github.com/fy0/gorbac/v3/filter"
)

func TestSchemaFromStruct_ScalarFields(t *testing.T) {
	type projectRow struct {
		ProjectID  int64  `json:"project_id" db:"id"`
		CreatorID  int64  `json:"creator_id"`
		Visibility string `json:"visibility"`
		Name       string `json:"name" filter:",contains"`

		// Unsupported types should be ignored unless explicitly tagged.
		Extra []string `json:"extra"`

		unexported string
	}

	row := projectRow{unexported: "ignored"}
	schema, err := filter.SchemaFromStruct("test_project", "p", row)
	if err != nil {
		t.Fatal(err)
	}

	if schema.Name != "test_project" {
		t.Fatalf("unexpected schema name: %q", schema.Name)
	}

	projectID, ok := schema.Fields["project_id"]
	if !ok {
		t.Fatalf("missing project_id field: %#v", schema.Fields)
	}
	if projectID.Column.Table != "p" || projectID.Column.Name != "id" {
		t.Fatalf("unexpected project_id column: %#v", projectID.Column)
	}
	if projectID.Type != filter.FieldTypeInt || projectID.Kind != filter.FieldKindScalar {
		t.Fatalf("unexpected project_id def: %#v", projectID)
	}

	name := schema.Fields["name"]
	if !name.SupportsContains {
		t.Fatalf("expected name.SupportsContains=true, got %#v", name.SupportsContains)
	}

	if _, ok := schema.Fields["extra"]; ok {
		t.Fatalf("expected extra to be ignored, got %#v", schema.Fields["extra"])
	}
	if _, ok := schema.Fields["unexported"]; ok {
		t.Fatalf("expected unexported field to be ignored, got %#v", schema.Fields["unexported"])
	}

	engine, err := filter.NewEngine(schema)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Compile(`project_id > 0 && name.contains("infra")`)
	if err != nil {
		t.Fatalf("expected expression to compile, got %v", err)
	}
}

func TestSchemaFromStruct_EmbeddedStruct(t *testing.T) {
	type base struct {
		CreatorID int64 `json:"creator_id" db:"creator_id"`
	}
	type projectRow struct {
		base
		ProjectID int64 `json:"project_id" db:"id"`
	}

	schema, err := filter.SchemaFromStruct("embedded", "t", projectRow{})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := schema.Fields["creator_id"]; !ok {
		t.Fatalf("expected embedded field creator_id, got %#v", schema.Fields)
	}
	if _, ok := schema.Fields["project_id"]; !ok {
		t.Fatalf("expected field project_id, got %#v", schema.Fields)
	}
}

func TestSchemaFromStruct_JSONKinds(t *testing.T) {
	type payload struct {
		Tags []string `filter:"tags,kind=json_list,column=payload,json=tags"`
		Tag  string   `filter:"tag,kind=virtual_alias,alias=tags"`

		HasTaskList bool `filter:"has_task_list,kind=json_bool,column=payload,json=property.hasTaskList"`
	}

	schema, err := filter.SchemaFromStruct("json", "t", payload{})
	if err != nil {
		t.Fatal(err)
	}

	tags := schema.Fields["tags"]
	if tags.Kind != filter.FieldKindJSONList || tags.Type != filter.FieldTypeString {
		t.Fatalf("unexpected tags field: %#v", tags)
	}
	if tags.Column.Table != "t" || tags.Column.Name != "payload" {
		t.Fatalf("unexpected tags column: %#v", tags.Column)
	}
	if len(tags.JSONPath) != 1 || tags.JSONPath[0] != "tags" {
		t.Fatalf("unexpected tags JSONPath: %#v", tags.JSONPath)
	}

	tag := schema.Fields["tag"]
	if tag.Kind != filter.FieldKindVirtualAlias || tag.AliasFor != "tags" {
		t.Fatalf("unexpected tag alias: %#v", tag)
	}

	engine, err := filter.NewEngine(schema)
	if err != nil {
		t.Fatal(err)
	}

	expr := `"foo" in tags && has_task_list && tag in ["foo"] && tags.exists(t, t.contains("bar"))`
	_, err = engine.Compile(expr)
	if err != nil {
		t.Fatalf("expected expression to compile, got %v", err)
	}
}
