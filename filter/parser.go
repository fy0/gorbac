package filter

import (
	"fmt"
	"time"

	exprv1 "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func buildCondition(expr *exprv1.Expr, schema Schema, predicates map[string]SQLPredicate) (Condition, error) {
	switch v := expr.ExprKind.(type) {
	case *exprv1.Expr_CallExpr:
		return buildCallCondition(v.CallExpr, schema, predicates)
	case *exprv1.Expr_ConstExpr:
		val, err := getConstValue(expr)
		if err != nil {
			return nil, err
		}
		switch v := val.(type) {
		case bool:
			return &ConstantCondition{Value: v}, nil
		case int64:
			return &ConstantCondition{Value: v != 0}, nil
		case float64:
			return &ConstantCondition{Value: v != 0}, nil
		default:
			return nil, fmt.Errorf("filter must evaluate to a boolean value")
		}
	case *exprv1.Expr_IdentExpr:
		name := v.IdentExpr.GetName()
		field, ok := schema.Field(name)
		if !ok {
			return nil, fmt.Errorf("unknown identifier %q", name)
		}
		if field.Type != FieldTypeBool {
			return nil, fmt.Errorf("identifier %q is not boolean", name)
		}
		return &FieldPredicateCondition{Field: name}, nil
	case *exprv1.Expr_ComprehensionExpr:
		return buildComprehensionCondition(v.ComprehensionExpr, schema)
	default:
		return nil, fmt.Errorf("unsupported top-level expression")
	}
}

func buildCallCondition(call *exprv1.Expr_Call, schema Schema, predicates map[string]SQLPredicate) (Condition, error) {
	switch call.Function {
	case "_&&_":
		if len(call.Args) != 2 {
			return nil, fmt.Errorf("logical AND expects two arguments")
		}
		left, err := buildCondition(call.Args[0], schema, predicates)
		if err != nil {
			return nil, err
		}
		right, err := buildCondition(call.Args[1], schema, predicates)
		if err != nil {
			return nil, err
		}
		return &LogicalCondition{Operator: LogicalAnd, Left: left, Right: right}, nil

	case "_||_":
		if len(call.Args) != 2 {
			return nil, fmt.Errorf("logical OR expects two arguments")
		}
		left, err := buildCondition(call.Args[0], schema, predicates)
		if err != nil {
			return nil, err
		}
		right, err := buildCondition(call.Args[1], schema, predicates)
		if err != nil {
			return nil, err
		}
		return &LogicalCondition{Operator: LogicalOr, Left: left, Right: right}, nil

	case "!_":
		if len(call.Args) != 1 {
			return nil, fmt.Errorf("logical NOT expects one argument")
		}
		child, err := buildCondition(call.Args[0], schema, predicates)
		if err != nil {
			return nil, err
		}
		return &NotCondition{Expr: child}, nil

	case "_==_", "_!=_", "_<_", "_>_", "_<=_", "_>=_":
		return buildComparisonCondition(call, schema)

	case "@in":
		return buildInCondition(call, schema)

	case "contains":
		return buildContainsCondition(call, schema)

	case "startsWith":
		return buildStartsWithCondition(call, schema)

	case "endsWith":
		return buildEndsWithCondition(call, schema)

	case "sql":
		return buildSQLPredicateCondition(call, schema, predicates)

	default:
		return nil, fmt.Errorf("unsupported call expression %q", call.Function)
	}
}

func buildSQLPredicateCondition(call *exprv1.Expr_Call, schema Schema, predicates map[string]SQLPredicate) (Condition, error) {
	if predicates == nil {
		return nil, fmt.Errorf("sql() is not enabled (no predicates registered)")
	}
	if len(call.Args) != 1 && len(call.Args) != 2 {
		return nil, fmt.Errorf("sql() expects 1 or 2 arguments")
	}

	nameAny, err := getConstValue(call.Args[0])
	if err != nil {
		return nil, fmt.Errorf("sql() predicate name must be a string literal")
	}
	name, ok := nameAny.(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("sql() predicate name must be a non-empty string literal")
	}
	pred, ok := predicates[name]
	if !ok {
		return nil, fmt.Errorf("unknown sql predicate %q", name)
	}

	args := []ValueExpr(nil)
	if len(call.Args) == 2 {
		listExpr := call.Args[1].GetListExpr()
		if listExpr == nil {
			return nil, fmt.Errorf("sql() args must be a list literal")
		}
		args = make([]ValueExpr, 0, len(listExpr.Elements))
		for _, elem := range listExpr.Elements {
			v, err := buildValueExpr(elem, schema)
			if err != nil {
				return nil, err
			}
			switch v.(type) {
			case *LiteralValue, *ParamRef:
				// ok
			default:
				return nil, fmt.Errorf("sql() args must be literals or params")
			}
			args = append(args, v)
		}
	}

	return &SQLPredicateCondition{
		Name: name,
		SQL:  pred.SQL,
		Args: args,
		Eval: pred.Eval,
	}, nil
}

func buildComparisonCondition(call *exprv1.Expr_Call, schema Schema) (Condition, error) {
	if len(call.Args) != 2 {
		return nil, fmt.Errorf("comparison expects two arguments")
	}
	op, err := toComparisonOperator(call.Function)
	if err != nil {
		return nil, err
	}

	left, err := buildValueExpr(call.Args[0], schema)
	if err != nil {
		return nil, err
	}
	right, err := buildValueExpr(call.Args[1], schema)
	if err != nil {
		return nil, err
	}

	// If the left side is a field, validate allowed operators.
	if field, ok := left.(*FieldRef); ok {
		def, exists := schema.Field(field.Name)
		if !exists {
			return nil, fmt.Errorf("unknown identifier %q", field.Name)
		}
		if def.AllowedComparisonOps != nil {
			if _, allowed := def.AllowedComparisonOps[op]; !allowed {
				return nil, fmt.Errorf("operator %s not allowed for field %q", op, field.Name)
			}
		}
	}

	return &ComparisonCondition{
		Left:     left,
		Operator: op,
		Right:    right,
	}, nil
}

func buildInCondition(call *exprv1.Expr_Call, schema Schema) (Condition, error) {
	if len(call.Args) != 2 {
		return nil, fmt.Errorf("in operator expects two arguments")
	}

	// Handle: element in json_list_field
	if identName, err := getIdentName(call.Args[1]); err == nil {
		if field, ok := schema.Field(identName); ok {
			if field.Kind == FieldKindVirtualAlias {
				resolved, ok := schema.ResolveAlias(identName)
				if !ok {
					return nil, fmt.Errorf("invalid alias %q", identName)
				}
				field = resolved
			}
			if field.Kind == FieldKindJSONList {
				element, err := buildValueExpr(call.Args[0], schema)
				if err != nil {
					return nil, err
				}
				return &ElementInCondition{
					Element: element,
					Field:   identName,
				}, nil
			}
		}
	}

	left, err := buildValueExpr(call.Args[0], schema)
	if err != nil {
		return nil, err
	}

	if listExpr := call.Args[1].GetListExpr(); listExpr != nil {
		values := make([]ValueExpr, 0, len(listExpr.Elements))
		for _, element := range listExpr.Elements {
			value, err := buildValueExpr(element, schema)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return &InCondition{Left: left, Values: values}, nil
	}

	// Allow: `field in some_list_param`.
	right, err := buildValueExpr(call.Args[1], schema)
	if err != nil {
		return nil, err
	}
	return &InCondition{Left: left, Values: []ValueExpr{right}}, nil
}

func buildContainsCondition(call *exprv1.Expr_Call, schema Schema) (Condition, error) {
	if call.Target == nil {
		return nil, fmt.Errorf("contains requires a target")
	}
	targetName, err := getIdentName(call.Target)
	if err != nil {
		return nil, err
	}

	field, ok := schema.Field(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown identifier %q", targetName)
	}
	if !field.SupportsContains {
		return nil, fmt.Errorf("identifier %q does not support contains()", targetName)
	}
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("contains expects exactly one argument")
	}
	value, err := buildValueExpr(call.Args[0], schema)
	if err != nil {
		return nil, err
	}
	switch value.(type) {
	case *LiteralValue, *ParamRef:
		// ok
	default:
		return nil, fmt.Errorf("contains argument must be a literal or param")
	}

	return &ContainsCondition{
		Field: targetName,
		Value: value,
	}, nil
}

func buildStartsWithCondition(call *exprv1.Expr_Call, schema Schema) (Condition, error) {
	if call.Target == nil {
		return nil, fmt.Errorf("startsWith requires a target")
	}
	targetName, err := getIdentName(call.Target)
	if err != nil {
		return nil, err
	}

	field, ok := schema.Field(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown identifier %q", targetName)
	}
	if !field.SupportsContains {
		return nil, fmt.Errorf("identifier %q does not support startsWith()", targetName)
	}
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("startsWith expects exactly one argument")
	}
	value, err := buildValueExpr(call.Args[0], schema)
	if err != nil {
		return nil, err
	}
	switch value.(type) {
	case *LiteralValue, *ParamRef:
		// ok
	default:
		return nil, fmt.Errorf("startsWith argument must be a literal or param")
	}

	return &StartsWithCondition{
		Field: targetName,
		Value: value,
	}, nil
}

func buildEndsWithCondition(call *exprv1.Expr_Call, schema Schema) (Condition, error) {
	if call.Target == nil {
		return nil, fmt.Errorf("endsWith requires a target")
	}
	targetName, err := getIdentName(call.Target)
	if err != nil {
		return nil, err
	}

	field, ok := schema.Field(targetName)
	if !ok {
		return nil, fmt.Errorf("unknown identifier %q", targetName)
	}
	if !field.SupportsContains {
		return nil, fmt.Errorf("identifier %q does not support endsWith()", targetName)
	}
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("endsWith expects exactly one argument")
	}
	value, err := buildValueExpr(call.Args[0], schema)
	if err != nil {
		return nil, err
	}
	switch value.(type) {
	case *LiteralValue, *ParamRef:
		// ok
	default:
		return nil, fmt.Errorf("endsWith argument must be a literal or param")
	}

	return &EndsWithCondition{
		Field: targetName,
		Value: value,
	}, nil
}

func buildValueExpr(expr *exprv1.Expr, schema Schema) (ValueExpr, error) {
	if identName, err := getIdentName(expr); err == nil {
		if _, ok := schema.Field(identName); ok {
			return &FieldRef{Name: identName}, nil
		}
		return &ParamRef{Name: identName}, nil
	}

	if literal, err := getConstValue(expr); err == nil {
		return &LiteralValue{Value: literal}, nil
	}

	if value, ok, err := evaluateNumeric(expr); err != nil {
		return nil, err
	} else if ok {
		return &LiteralValue{Value: value}, nil
	}

	if call := expr.GetCallExpr(); call != nil {
		switch call.Function {
		case "size":
			if len(call.Args) != 1 {
				return nil, fmt.Errorf("size() expects one argument")
			}
			arg, err := buildValueExpr(call.Args[0], schema)
			if err != nil {
				return nil, err
			}
			return &FunctionValue{
				Name: "size",
				Args: []ValueExpr{arg},
			}, nil
		}
	}

	return nil, fmt.Errorf("unsupported value expression")
}

func toComparisonOperator(fn string) (ComparisonOperator, error) {
	switch fn {
	case "_==_":
		return CompareEq, nil
	case "_!=_":
		return CompareNeq, nil
	case "_<_":
		return CompareLt, nil
	case "_>_":
		return CompareGt, nil
	case "_<=_":
		return CompareLte, nil
	case "_>=_":
		return CompareGte, nil
	default:
		return "", fmt.Errorf("unsupported comparison operator %q", fn)
	}
}

func getIdentName(expr *exprv1.Expr) (string, error) {
	if ident := expr.GetIdentExpr(); ident != nil {
		return ident.GetName(), nil
	}
	return "", fmt.Errorf("expression is not an identifier")
}

func getConstValue(expr *exprv1.Expr) (any, error) {
	v, ok := expr.ExprKind.(*exprv1.Expr_ConstExpr)
	if !ok {
		return nil, fmt.Errorf("expression is not a literal")
	}
	switch x := v.ConstExpr.ConstantKind.(type) {
	case *exprv1.Constant_StringValue:
		return v.ConstExpr.GetStringValue(), nil
	case *exprv1.Constant_Int64Value:
		return v.ConstExpr.GetInt64Value(), nil
	case *exprv1.Constant_Uint64Value:
		return int64(v.ConstExpr.GetUint64Value()), nil
	case *exprv1.Constant_DoubleValue:
		return v.ConstExpr.GetDoubleValue(), nil
	case *exprv1.Constant_BoolValue:
		return v.ConstExpr.GetBoolValue(), nil
	case *exprv1.Constant_NullValue:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported constant %T", x)
	}
}

func evaluateNumeric(expr *exprv1.Expr) (int64, bool, error) {
	if literal, err := getConstValue(expr); err == nil {
		switch v := literal.(type) {
		case int64:
			return v, true, nil
		case float64:
			return int64(v), true, nil
		default:
			return 0, false, nil
		}
	}

	call := expr.GetCallExpr()
	if call == nil {
		return 0, false, nil
	}

	switch call.Function {
	case "now":
		return timeNowUnix(), true, nil
	case "_+_", "_-_", "_*_":
		if len(call.Args) != 2 {
			return 0, false, fmt.Errorf("numeric %q expects two arguments", call.Function)
		}
		left, ok, err := evaluateNumeric(call.Args[0])
		if err != nil || !ok {
			return 0, ok, err
		}
		right, ok, err := evaluateNumeric(call.Args[1])
		if err != nil || !ok {
			return 0, ok, err
		}
		switch call.Function {
		case "_+_":
			return left + right, true, nil
		case "_-_":
			return left - right, true, nil
		case "_*_":
			return left * right, true, nil
		}
	case "-_":
		if len(call.Args) != 1 {
			return 0, false, fmt.Errorf("unary negation expects one argument")
		}
		val, ok, err := evaluateNumeric(call.Args[0])
		if err != nil || !ok {
			return 0, ok, err
		}
		return -val, true, nil
	}

	return 0, false, nil
}

func timeNowUnix() int64 {
	return time.Now().Unix()
}

func buildComprehensionCondition(comp *exprv1.Expr_Comprehension, schema Schema) (Condition, error) {
	kind, err := detectComprehensionKind(comp)
	if err != nil {
		return nil, err
	}

	iterRangeIdent := comp.IterRange.GetIdentExpr()
	if iterRangeIdent == nil {
		return nil, fmt.Errorf("comprehension range must be a field identifier")
	}
	fieldName := iterRangeIdent.GetName()

	field, ok := schema.Field(fieldName)
	if !ok {
		return nil, fmt.Errorf("unknown field %q in comprehension", fieldName)
	}
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := schema.ResolveAlias(fieldName)
		if !ok {
			return nil, fmt.Errorf("invalid alias %q", fieldName)
		}
		field = resolved
	}
	if field.Kind != FieldKindJSONList {
		return nil, fmt.Errorf("field %q does not support comprehension (must be a json list)", fieldName)
	}

	predicate, err := extractPredicate(comp, schema)
	if err != nil {
		return nil, err
	}

	return &ListComprehensionCondition{
		Kind:      kind,
		Field:     fieldName,
		IterVar:   comp.IterVar,
		Predicate: predicate,
	}, nil
}

func detectComprehensionKind(comp *exprv1.Expr_Comprehension) (ComprehensionKind, error) {
	accuInit := comp.AccuInit.GetConstExpr()
	if accuInit == nil {
		return "", fmt.Errorf("comprehension accumulator must be initialized with a constant")
	}

	// exists() starts with false and uses OR (||) in loop step
	if !accuInit.GetBoolValue() {
		if step := comp.LoopStep.GetCallExpr(); step != nil && step.Function == "_||_" {
			return ComprehensionExists, nil
		}
	}

	// all() starts with true and uses AND (&&) - not supported
	if accuInit.GetBoolValue() {
		if step := comp.LoopStep.GetCallExpr(); step != nil && step.Function == "_&&_" {
			return "", fmt.Errorf("all() comprehension is not supported; use exists() instead")
		}
	}

	return "", fmt.Errorf("unsupported comprehension type; only exists() is supported")
}

func extractPredicate(comp *exprv1.Expr_Comprehension, schema Schema) (PredicateExpr, error) {
	step := comp.LoopStep.GetCallExpr()
	if step == nil {
		return nil, fmt.Errorf("comprehension loop step must be a call expression")
	}
	if len(step.Args) != 2 {
		return nil, fmt.Errorf("comprehension loop step must have two arguments")
	}

	predicateExpr := step.Args[1]
	predicateCall := predicateExpr.GetCallExpr()
	if predicateCall == nil {
		return nil, fmt.Errorf("comprehension predicate must be a call expression")
	}

	// Supported patterns:
	//   t.startsWith(x)
	//   t.endsWith(x)
	//   t.contains(x)
	if predicateCall.Target == nil {
		return nil, fmt.Errorf("predicate requires a target")
	}
	targetIdent := predicateCall.Target.GetIdentExpr()
	if targetIdent == nil {
		return nil, fmt.Errorf("predicate target must be an identifier")
	}
	if targetIdent.GetName() != comp.IterVar {
		return nil, fmt.Errorf("predicate target must be iteration variable %q", comp.IterVar)
	}
	if len(predicateCall.Args) != 1 {
		return nil, fmt.Errorf("predicate %q expects one argument", predicateCall.Function)
	}
	arg, err := buildValueExpr(predicateCall.Args[0], schema)
	if err != nil {
		return nil, err
	}
	switch arg.(type) {
	case *LiteralValue, *ParamRef:
		// ok
	default:
		return nil, fmt.Errorf("predicate argument must be a literal or param")
	}

	switch predicateCall.Function {
	case "startsWith":
		return &StartsWithPredicate{Prefix: arg}, nil
	case "endsWith":
		return &EndsWithPredicate{Suffix: arg}, nil
	case "contains":
		return &ContainsPredicate{Substring: arg}, nil
	default:
		return nil, fmt.Errorf("unsupported predicate function %q", predicateCall.Function)
	}
}
