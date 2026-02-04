package filter

import (
	"fmt"
	"strings"
)

// EvalOptions configures in-memory evaluation.
//
// Dialect is optional, but when set it will try to mirror renderer semantics
// for a few dialect-sensitive operations (e.g. Postgres ILIKE for contains()).
type EvalOptions struct {
}

// EvaluateCondition evaluates a compiled condition tree against the provided vars.
//
// vars keys are CEL identifiers (schema field names) and any param variables (bindings).
func EvaluateCondition(schema Schema, cond Condition, vars map[string]any, opts EvalOptions) (bool, error) {
	if vars == nil {
		vars = map[string]any{}
	}

	switch c := cond.(type) {
	case *LogicalCondition:
		left, err := EvaluateCondition(schema, c.Left, vars, opts)
		if err != nil {
			return false, err
		}
		switch c.Operator {
		case LogicalAnd:
			if !left {
				return false, nil
			}
			return EvaluateCondition(schema, c.Right, vars, opts)
		case LogicalOr:
			if left {
				return true, nil
			}
			return EvaluateCondition(schema, c.Right, vars, opts)
		default:
			return false, fmt.Errorf("unsupported logical operator %s", c.Operator)
		}

	case *NotCondition:
		val, err := EvaluateCondition(schema, c.Expr, vars, opts)
		if err != nil {
			return false, err
		}
		return !val, nil

	case *FieldPredicateCondition:
		value, ok := vars[c.Field]
		if !ok {
			return false, fmt.Errorf("missing value for field %q", c.Field)
		}
		b, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("field %q expects bool value, got %T", c.Field, value)
		}
		return b, nil

	case *ComparisonCondition:
		return evalComparison(schema, c, vars)

	case *InCondition:
		return evalIn(schema, c, vars)

	case *ElementInCondition:
		return evalElementIn(schema, c, vars)

	case *ContainsCondition:
		return evalContains(schema, c, vars, opts)

	case *StartsWithCondition:
		return evalStartsWith(schema, c, vars)

	case *EndsWithCondition:
		return evalEndsWith(schema, c, vars)

	case *ListComprehensionCondition:
		return evalListComprehension(schema, c, vars)

	case *SQLPredicateCondition:
		if c.Eval == nil {
			return false, fmt.Errorf("sql predicate %q does not support in-memory evaluation", c.Name)
		}
		args := make([]any, 0, len(c.Args))
		for _, expr := range c.Args {
			v, err := evalValueExpr(schema, expr, vars)
			if err != nil {
				return false, err
			}
			args = append(args, v)
		}
		return c.Eval(schema, vars, args, opts)

	case *ConstantCondition:
		return c.Value, nil

	default:
		return false, fmt.Errorf("unsupported condition type %T", cond)
	}
}

func evalComparison(schema Schema, cond *ComparisonCondition, vars map[string]any) (bool, error) {
	left, err := evalValueExpr(schema, cond.Left, vars)
	if err != nil {
		return false, err
	}
	right, err := evalValueExpr(schema, cond.Right, vars)
	if err != nil {
		return false, err
	}

	// Null comparisons are only allowed for eq/neq in our renderer too.
	if left == nil || right == nil {
		switch cond.Operator {
		case CompareEq:
			return left == right, nil
		case CompareNeq:
			return left != right, nil
		default:
			return false, fmt.Errorf("operator %s not supported for null comparison", cond.Operator)
		}
	}

	switch l := left.(type) {
	case string:
		r, ok := right.(string)
		if !ok {
			return false, fmt.Errorf("comparison type mismatch: %T %s %T", left, cond.Operator, right)
		}
		switch cond.Operator {
		case CompareEq:
			return l == r, nil
		case CompareNeq:
			return l != r, nil
		case CompareLt:
			return l < r, nil
		case CompareLte:
			return l <= r, nil
		case CompareGt:
			return l > r, nil
		case CompareGte:
			return l >= r, nil
		default:
			return false, fmt.Errorf("unsupported string operator %s", cond.Operator)
		}

	case bool:
		r, ok := right.(bool)
		if !ok {
			return false, fmt.Errorf("comparison type mismatch: %T %s %T", left, cond.Operator, right)
		}
		switch cond.Operator {
		case CompareEq:
			return l == r, nil
		case CompareNeq:
			return l != r, nil
		default:
			return false, fmt.Errorf("unsupported bool operator %s", cond.Operator)
		}

	default:
		ln, err := toInt64(left)
		if err != nil {
			return false, fmt.Errorf("comparison expects numeric values: %w", err)
		}
		rn, err := toInt64(right)
		if err != nil {
			return false, fmt.Errorf("comparison expects numeric values: %w", err)
		}
		switch cond.Operator {
		case CompareEq:
			return ln == rn, nil
		case CompareNeq:
			return ln != rn, nil
		case CompareLt:
			return ln < rn, nil
		case CompareLte:
			return ln <= rn, nil
		case CompareGt:
			return ln > rn, nil
		case CompareGte:
			return ln >= rn, nil
		default:
			return false, fmt.Errorf("unsupported numeric operator %s", cond.Operator)
		}
	}
}

func evalIn(schema Schema, cond *InCondition, vars map[string]any) (bool, error) {
	// Support virtual alias (string) membership checks on a JSON list.
	if leftField, ok := cond.Left.(*FieldRef); ok {
		if field, ok := schema.Field(leftField.Name); ok && field.Kind == FieldKindVirtualAlias {
			resolved, ok := schema.ResolveAlias(leftField.Name)
			if !ok {
				return false, fmt.Errorf("invalid alias %q", leftField.Name)
			}
			if resolved.Kind == FieldKindJSONList {
				listRaw, ok := vars[resolved.Name]
				if !ok || listRaw == nil {
					return false, nil
				}
				list, ok := toAnySlice(listRaw)
				if !ok {
					return false, fmt.Errorf("field %q expects a slice/array value, got %T", resolved.Name, listRaw)
				}

				rightFlat := make([]any, 0, len(cond.Values))
				for _, v := range cond.Values {
					right, err := evalValueExpr(schema, v, vars)
					if err != nil {
						return false, err
					}
					if expanded, ok := toAnySlice(right); ok {
						rightFlat = append(rightFlat, expanded...)
						continue
					}
					rightFlat = append(rightFlat, right)
				}

				hierarchical := leftField.Name == "tag"
				for _, item := range list {
					itemStr, ok := item.(string)
					if !ok {
						return false, fmt.Errorf("field %q expects string elements, got %T", resolved.Name, item)
					}
					for _, candidate := range rightFlat {
						candStr, ok := candidate.(string)
						if !ok {
							return false, fmt.Errorf("alias %q expects string values, got %T", leftField.Name, candidate)
						}
						if itemStr == candStr {
							return true, nil
						}
						if hierarchical && strings.HasPrefix(itemStr, candStr+"/") {
							return true, nil
						}
					}
				}

				return false, nil
			}
		}
	}

	left, err := evalValueExpr(schema, cond.Left, vars)
	if err != nil {
		return false, err
	}

	for _, v := range cond.Values {
		right, err := evalValueExpr(schema, v, vars)
		if err != nil {
			return false, err
		}
		if list, ok := toAnySlice(right); ok {
			for _, item := range list {
				if equalsLoose(left, item) {
					return true, nil
				}
			}
			continue
		}
		if equalsLoose(left, right) {
			return true, nil
		}
	}

	return false, nil
}

func evalElementIn(schema Schema, cond *ElementInCondition, vars map[string]any) (bool, error) {
	field, ok := schema.Field(cond.Field)
	if !ok {
		return false, fmt.Errorf("unknown field %q", cond.Field)
	}
	fieldName := cond.Field
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := schema.ResolveAlias(cond.Field)
		if !ok {
			return false, fmt.Errorf("invalid alias %q", cond.Field)
		}
		field = resolved
		fieldName = resolved.Name
	}
	if field.Kind != FieldKindJSONList {
		return false, fmt.Errorf("field %q is not a JSON list", cond.Field)
	}

	listRaw, ok := vars[fieldName]
	if !ok || listRaw == nil {
		return false, nil
	}
	list, ok := toAnySlice(listRaw)
	if !ok {
		return false, fmt.Errorf("field %q expects a slice/array value, got %T", fieldName, listRaw)
	}

	element, err := evalValueExpr(schema, cond.Element, vars)
	if err != nil {
		return false, err
	}
	for _, item := range list {
		if equalsLoose(element, item) {
			return true, nil
		}
	}
	return false, nil
}

func evalListComprehension(schema Schema, cond *ListComprehensionCondition, vars map[string]any) (bool, error) {
	if cond.Kind != ComprehensionExists {
		return false, fmt.Errorf("unsupported comprehension kind %q", cond.Kind)
	}

	field, ok := schema.Field(cond.Field)
	if !ok {
		return false, fmt.Errorf("unknown field %q", cond.Field)
	}
	fieldName := cond.Field
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := schema.ResolveAlias(cond.Field)
		if !ok {
			return false, fmt.Errorf("invalid alias %q", cond.Field)
		}
		field = resolved
		fieldName = resolved.Name
	}
	if field.Kind != FieldKindJSONList {
		return false, fmt.Errorf("field %q is not a JSON list", cond.Field)
	}

	listRaw, ok := vars[fieldName]
	if !ok || listRaw == nil {
		return false, nil
	}
	list, ok := toAnySlice(listRaw)
	if !ok {
		return false, fmt.Errorf("field %q expects a slice/array value, got %T", fieldName, listRaw)
	}

	switch pred := cond.Predicate.(type) {
	case *StartsWithPredicate:
		prefixRaw, err := evalValueExpr(schema, pred.Prefix, vars)
		if err != nil {
			return false, err
		}
		prefix, ok := prefixRaw.(string)
		if !ok {
			return false, fmt.Errorf("startsWith expects string prefix, got %T", prefixRaw)
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return false, fmt.Errorf("field %q expects string elements, got %T", fieldName, item)
			}
			if strings.HasPrefix(s, prefix) {
				return true, nil
			}
		}
		return false, nil
	case *EndsWithPredicate:
		suffixRaw, err := evalValueExpr(schema, pred.Suffix, vars)
		if err != nil {
			return false, err
		}
		suffix, ok := suffixRaw.(string)
		if !ok {
			return false, fmt.Errorf("endsWith expects string suffix, got %T", suffixRaw)
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return false, fmt.Errorf("field %q expects string elements, got %T", fieldName, item)
			}
			if strings.HasSuffix(s, suffix) {
				return true, nil
			}
		}
		return false, nil
	case *ContainsPredicate:
		subRaw, err := evalValueExpr(schema, pred.Substring, vars)
		if err != nil {
			return false, err
		}
		sub, ok := subRaw.(string)
		if !ok {
			return false, fmt.Errorf("contains expects string substring, got %T", subRaw)
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return false, fmt.Errorf("field %q expects string elements, got %T", fieldName, item)
			}
			if strings.Contains(s, sub) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported predicate type %T", pred)
	}
}

func evalContains(schema Schema, cond *ContainsCondition, vars map[string]any, opts EvalOptions) (bool, error) {
	raw, ok := vars[cond.Field]
	if !ok {
		return false, fmt.Errorf("missing value for field %q", cond.Field)
	}
	str, ok := raw.(string)
	if !ok {
		return false, fmt.Errorf("contains() requires string field %q, got %T", cond.Field, raw)
	}

	needleRaw, err := evalValueExpr(schema, cond.Value, vars)
	if err != nil {
		return false, err
	}
	needle, ok := needleRaw.(string)
	if !ok {
		return false, fmt.Errorf("contains() requires string needle, got %T", needleRaw)
	}

	return strings.Contains(str, needle), nil
}

func evalStartsWith(schema Schema, cond *StartsWithCondition, vars map[string]any) (bool, error) {
	raw, ok := vars[cond.Field]
	if !ok {
		return false, fmt.Errorf("missing value for field %q", cond.Field)
	}
	str, ok := raw.(string)
	if !ok {
		return false, fmt.Errorf("startsWith() requires string field %q, got %T", cond.Field, raw)
	}

	prefixRaw, err := evalValueExpr(schema, cond.Value, vars)
	if err != nil {
		return false, err
	}
	prefix, ok := prefixRaw.(string)
	if !ok {
		return false, fmt.Errorf("startsWith() requires string prefix, got %T", prefixRaw)
	}
	if prefix == "" {
		return true, nil
	}

	return strings.HasPrefix(str, prefix), nil
}

func evalEndsWith(schema Schema, cond *EndsWithCondition, vars map[string]any) (bool, error) {
	raw, ok := vars[cond.Field]
	if !ok {
		return false, fmt.Errorf("missing value for field %q", cond.Field)
	}
	str, ok := raw.(string)
	if !ok {
		return false, fmt.Errorf("endsWith() requires string field %q, got %T", cond.Field, raw)
	}

	suffixRaw, err := evalValueExpr(schema, cond.Value, vars)
	if err != nil {
		return false, err
	}
	suffix, ok := suffixRaw.(string)
	if !ok {
		return false, fmt.Errorf("endsWith() requires string suffix, got %T", suffixRaw)
	}
	if suffix == "" {
		return true, nil
	}

	return strings.HasSuffix(str, suffix), nil
}

func evalValueExpr(schema Schema, expr ValueExpr, vars map[string]any) (any, error) {
	switch e := expr.(type) {
	case *FieldRef:
		v, ok := vars[e.Name]
		if !ok {
			return nil, fmt.Errorf("missing value for field %q", e.Name)
		}
		return v, nil
	case *ParamRef:
		v, ok := vars[e.Name]
		if !ok {
			return nil, fmt.Errorf("missing value for variable %q", e.Name)
		}
		return v, nil
	case *LiteralValue:
		return e.Value, nil
	case *FunctionValue:
		if e.Name != "size" {
			return nil, fmt.Errorf("unsupported function %q", e.Name)
		}
		if len(e.Args) != 1 {
			return nil, fmt.Errorf("size() expects one argument")
		}
		arg, err := evalValueExpr(schema, e.Args[0], vars)
		if err != nil {
			return nil, err
		}
		if arg == nil {
			return int64(0), nil
		}
		if list, ok := toAnySlice(arg); ok {
			return int64(len(list)), nil
		}
		return nil, fmt.Errorf("size() expects a slice/array argument, got %T", arg)
	default:
		return nil, fmt.Errorf("unsupported value expr %T", expr)
	}
}

func equalsLoose(left, right any) bool {
	if left == nil || right == nil {
		return left == right
	}

	switch l := left.(type) {
	case string:
		r, ok := right.(string)
		return ok && l == r
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	default:
		ln, err := toInt64(left)
		if err != nil {
			return false
		}
		rn, err := toInt64(right)
		if err != nil {
			return false
		}
		return ln == rn
	}
}
