package filter

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/cel-go/cel"
)

func walkTestSchema() Schema {
	return Schema{
		Name: "walk_test",
		Fields: map[string]*Field{
			"creator_id": {
				Name:        "creator_id",
				Type:        FieldTypeInt,
				Column:      Column{Table: "t", Name: "creator_id"},
				Expressions: map[DialectName]string{},
				AllowedComparisonOps: map[ComparisonOperator]bool{
					CompareEq: true,
				},
			},
			"visibility": {
				Name:        "visibility",
				Type:        FieldTypeString,
				Column:      Column{Table: "t", Name: "visibility"},
				Expressions: map[DialectName]string{},
				AllowedComparisonOps: map[ComparisonOperator]bool{
					CompareEq: true,
				},
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Variable("creator_id", cel.IntType),
			cel.Variable("visibility", cel.StringType),
		},
	}
}

func nodeName(node any) string {
	typeOf := reflect.TypeOf(node)
	if typeOf.Kind() == reflect.Pointer {
		return typeOf.Elem().Name()
	}
	return typeOf.Name()
}

func TestProgramSchema(t *testing.T) {
	schema := walkTestSchema()
	engine, err := NewEngine(schema)
	if err != nil {
		t.Fatal(err)
	}

	program, err := engine.Compile(`creator_id == 1 && visibility == "PUBLIC"`)
	if err != nil {
		t.Fatal(err)
	}

	got := program.Schema()
	if got.Name != schema.Name {
		t.Fatalf("unexpected schema name: want %q, got %q", schema.Name, got.Name)
	}
	if len(got.Fields) != len(schema.Fields) {
		t.Fatalf("unexpected field count: want %d, got %d", len(schema.Fields), len(got.Fields))
	}
	for name, wantField := range schema.Fields {
		gotField, ok := got.Fields[name]
		if !ok {
			t.Fatalf("missing field %q in returned schema", name)
		}
		if !reflect.DeepEqual(gotField, wantField) {
			t.Fatalf("unexpected field %q: want %#v, got %#v", name, wantField, gotField)
		}
	}
}

func TestWalkConditionNil(t *testing.T) {
	called := false
	if err := WalkCondition(nil, func(Condition) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected nil condition to skip callback")
	}

	var typedNil *LogicalCondition
	if err := WalkCondition(typedNil, func(Condition) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected typed nil condition to skip callback")
	}
}

func TestWalkConditionPreorderBinaryLogical(t *testing.T) {
	cond := &LogicalCondition{
		Operator: LogicalAnd,
		Left: &ComparisonCondition{
			Left:     &FieldRef{Name: "creator_id"},
			Operator: CompareEq,
			Right:    &LiteralValue{Value: int64(1)},
		},
		Right: &LogicalCondition{
			Operator: LogicalOr,
			Left:     &FieldPredicateCondition{Field: "visibility"},
			Right:    &ConstantCondition{Value: true},
		},
	}

	var visited []string
	err := WalkCondition(cond, func(cond Condition) error {
		visited = append(visited, nodeName(cond))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"LogicalCondition",
		"ComparisonCondition",
		"LogicalCondition",
		"FieldPredicateCondition",
		"ConstantCondition",
	}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("unexpected walk order: want %#v, got %#v", want, visited)
	}
}

func TestWalkConditionNestedNotComparisonIn(t *testing.T) {
	cond := &NotCondition{
		Expr: &LogicalCondition{
			Operator: LogicalAnd,
			Left: &ComparisonCondition{
				Left:     &FieldRef{Name: "creator_id"},
				Operator: CompareEq,
				Right:    &LiteralValue{Value: int64(1)},
			},
			Right: &InCondition{
				Left: &FieldRef{Name: "visibility"},
				Values: []ValueExpr{
					&LiteralValue{Value: "PUBLIC"},
					&LiteralValue{Value: "PRIVATE"},
				},
			},
		},
	}

	var visited []string
	err := WalkCondition(cond, func(cond Condition) error {
		visited = append(visited, nodeName(cond))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"NotCondition", "LogicalCondition", "ComparisonCondition", "InCondition"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("unexpected walk order: want %#v, got %#v", want, visited)
	}
}

func TestWalkConditionShortCircuitOnError(t *testing.T) {
	stop := errors.New("stop")
	cond := &LogicalCondition{
		Operator: LogicalAnd,
		Left: &ComparisonCondition{
			Left:     &FieldRef{Name: "creator_id"},
			Operator: CompareEq,
			Right:    &LiteralValue{Value: int64(1)},
		},
		Right: &ConstantCondition{Value: true},
	}

	var visited []string
	err := WalkCondition(cond, func(cond Condition) error {
		visited = append(visited, nodeName(cond))
		if len(visited) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("expected stop error, got %v", err)
	}

	want := []string{"LogicalCondition", "ComparisonCondition"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("unexpected visited nodes after short-circuit: want %#v, got %#v", want, visited)
	}
}

func TestWalkConditionVisitsSQLPredicateArgs(t *testing.T) {
	cond := &SQLPredicateCondition{
		Name: "project_member",
		Args: []ValueExpr{
			&FunctionValue{
				Name: "size",
				Args: []ValueExpr{&FieldRef{Name: "visibility"}},
			},
			&ParamRef{Name: "current_user_id"},
		},
	}

	var condVisited []string
	var valueVisited []string
	err := walkConditionInternal(cond, func(cond Condition) error {
		condVisited = append(condVisited, nodeName(cond))
		return nil
	}, func(expr ValueExpr) error {
		valueVisited = append(valueVisited, nodeName(expr))
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"SQLPredicateCondition"}; !reflect.DeepEqual(condVisited, want) {
		t.Fatalf("unexpected condition nodes: want %#v, got %#v", want, condVisited)
	}
	if want := []string{"FunctionValue", "FieldRef", "ParamRef"}; !reflect.DeepEqual(valueVisited, want) {
		t.Fatalf("unexpected value nodes: want %#v, got %#v", want, valueVisited)
	}
}

func TestWalkConditionVisitsListComprehensionPredicate(t *testing.T) {
	cond := &ListComprehensionCondition{
		Kind:    ComprehensionExists,
		Field:   "tags",
		IterVar: "tag",
		Predicate: &ContainsPredicate{
			Substring: &FunctionValue{
				Name: "lower",
				Args: []ValueExpr{&ParamRef{Name: "q"}},
			},
		},
	}

	var condVisited []string
	var predicateVisited []string
	var valueVisited []string
	err := walkConditionInternal(cond, func(cond Condition) error {
		condVisited = append(condVisited, nodeName(cond))
		return nil
	}, func(expr ValueExpr) error {
		valueVisited = append(valueVisited, nodeName(expr))
		return nil
	}, func(expr PredicateExpr) error {
		predicateVisited = append(predicateVisited, nodeName(expr))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"ListComprehensionCondition"}; !reflect.DeepEqual(condVisited, want) {
		t.Fatalf("unexpected condition nodes: want %#v, got %#v", want, condVisited)
	}
	if want := []string{"ContainsPredicate"}; !reflect.DeepEqual(predicateVisited, want) {
		t.Fatalf("unexpected predicate nodes: want %#v, got %#v", want, predicateVisited)
	}
	if want := []string{"FunctionValue", "ParamRef"}; !reflect.DeepEqual(valueVisited, want) {
		t.Fatalf("unexpected value nodes: want %#v, got %#v", want, valueVisited)
	}
}

func TestWalkValueExprPreorderAndArgs(t *testing.T) {
	expr := &FunctionValue{
		Name: "concat",
		Args: []ValueExpr{
			&FieldRef{Name: "visibility"},
			&FunctionValue{
				Name: "lower",
				Args: []ValueExpr{&ParamRef{Name: "q"}},
			},
		},
	}

	var visited []string
	err := WalkValueExpr(expr, func(expr ValueExpr) error {
		visited = append(visited, nodeName(expr))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"FunctionValue", "FieldRef", "FunctionValue", "ParamRef"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("unexpected value walk order: want %#v, got %#v", want, visited)
	}
}

func TestWalkValueExprNilAndShortCircuit(t *testing.T) {
	called := false
	if err := WalkValueExpr(nil, func(ValueExpr) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected nil value expr to skip callback")
	}

	stop := errors.New("stop")
	expr := &FunctionValue{
		Name: "concat",
		Args: []ValueExpr{
			&FunctionValue{
				Name: "lower",
				Args: []ValueExpr{&ParamRef{Name: "q"}},
			},
			&LiteralValue{Value: "PUBLIC"},
		},
	}

	var visited []string
	err := WalkValueExpr(expr, func(expr ValueExpr) error {
		visited = append(visited, nodeName(expr))
		if len(visited) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("expected stop error, got %v", err)
	}

	want := []string{"FunctionValue", "FunctionValue"}
	if !reflect.DeepEqual(visited, want) {
		t.Fatalf("unexpected value nodes after short-circuit: want %#v, got %#v", want, visited)
	}
}

func TestWalkPredicateExprVisitsNestedValueExpr(t *testing.T) {
	expr := &StartsWithPredicate{
		Prefix: &FunctionValue{
			Name: "lower",
			Args: []ValueExpr{&ParamRef{Name: "q"}},
		},
	}

	var predicateVisited []string
	var valueVisited []string
	err := walkPredicateExprInternal(expr, func(expr PredicateExpr) error {
		predicateVisited = append(predicateVisited, nodeName(expr))
		return nil
	}, func(expr ValueExpr) error {
		valueVisited = append(valueVisited, nodeName(expr))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"StartsWithPredicate"}; !reflect.DeepEqual(predicateVisited, want) {
		t.Fatalf("unexpected predicate nodes: want %#v, got %#v", want, predicateVisited)
	}
	if want := []string{"FunctionValue", "ParamRef"}; !reflect.DeepEqual(valueVisited, want) {
		t.Fatalf("unexpected value nodes: want %#v, got %#v", want, valueVisited)
	}
}

func TestWalkPredicateExprNilAndShortCircuit(t *testing.T) {
	called := false
	if err := WalkPredicateExpr(nil, func(PredicateExpr) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected nil predicate expr to skip callback")
	}

	stop := errors.New("stop")
	expr := &ContainsPredicate{
		Substring: &FunctionValue{
			Name: "lower",
			Args: []ValueExpr{&ParamRef{Name: "q"}},
		},
	}

	valueCalled := false
	err := walkPredicateExprInternal(expr, func(PredicateExpr) error {
		return stop
	}, func(ValueExpr) error {
		valueCalled = true
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("expected stop error, got %v", err)
	}
	if valueCalled {
		t.Fatal("expected predicate walk to short-circuit before visiting nested value exprs")
	}
}
