package filter

import "reflect"

// WalkCondition visits cond and its descendants in pre-order depth-first order.
//
// Nil inputs are ignored. If fn returns an error, traversal stops immediately
// and that error is returned.
func WalkCondition(cond Condition, fn func(Condition) error) error {
	return walkConditionInternal(cond, fn, nil, nil)
}

// WalkValueExpr visits expr and its descendants in pre-order depth-first order.
//
// Nil inputs are ignored. If fn returns an error, traversal stops immediately
// and that error is returned.
func WalkValueExpr(expr ValueExpr, fn func(ValueExpr) error) error {
	return walkValueExprInternal(expr, fn)
}

// WalkPredicateExpr visits expr and its descendants in pre-order depth-first order.
//
// Nil inputs are ignored. If fn returns an error, traversal stops immediately
// and that error is returned.
func WalkPredicateExpr(expr PredicateExpr, fn func(PredicateExpr) error) error {
	return walkPredicateExprInternal(expr, fn, nil)
}

func walkConditionInternal(cond Condition, condFn func(Condition) error, valueFn func(ValueExpr) error, predicateFn func(PredicateExpr) error) error {
	if isNilNode(cond) {
		return nil
	}
	if err := visitCondition(cond, condFn); err != nil {
		return err
	}

	switch c := cond.(type) {
	case *LogicalCondition:
		if err := walkConditionInternal(c.Left, condFn, valueFn, predicateFn); err != nil {
			return err
		}
		return walkConditionInternal(c.Right, condFn, valueFn, predicateFn)
	case *NotCondition:
		return walkConditionInternal(c.Expr, condFn, valueFn, predicateFn)
	case *ComparisonCondition:
		if err := walkValueExprInternal(c.Left, valueFn); err != nil {
			return err
		}
		return walkValueExprInternal(c.Right, valueFn)
	case *InCondition:
		if err := walkValueExprInternal(c.Left, valueFn); err != nil {
			return err
		}
		for _, value := range c.Values {
			if err := walkValueExprInternal(value, valueFn); err != nil {
				return err
			}
		}
		return nil
	case *ElementInCondition:
		return walkValueExprInternal(c.Element, valueFn)
	case *ContainsCondition:
		return walkValueExprInternal(c.Value, valueFn)
	case *StartsWithCondition:
		return walkValueExprInternal(c.Value, valueFn)
	case *EndsWithCondition:
		return walkValueExprInternal(c.Value, valueFn)
	case *SQLPredicateCondition:
		for _, arg := range c.Args {
			if err := walkValueExprInternal(arg, valueFn); err != nil {
				return err
			}
		}
		return nil
	case *ListComprehensionCondition:
		return walkPredicateExprInternal(c.Predicate, predicateFn, valueFn)
	default:
		return nil
	}
}

func walkValueExprInternal(expr ValueExpr, fn func(ValueExpr) error) error {
	if isNilNode(expr) {
		return nil
	}
	if err := visitValueExpr(expr, fn); err != nil {
		return err
	}

	switch e := expr.(type) {
	case *FunctionValue:
		for _, arg := range e.Args {
			if err := walkValueExprInternal(arg, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkPredicateExprInternal(expr PredicateExpr, predicateFn func(PredicateExpr) error, valueFn func(ValueExpr) error) error {
	if isNilNode(expr) {
		return nil
	}
	if err := visitPredicateExpr(expr, predicateFn); err != nil {
		return err
	}

	switch e := expr.(type) {
	case *StartsWithPredicate:
		return walkValueExprInternal(e.Prefix, valueFn)
	case *EndsWithPredicate:
		return walkValueExprInternal(e.Suffix, valueFn)
	case *ContainsPredicate:
		return walkValueExprInternal(e.Substring, valueFn)
	default:
		return nil
	}
}

func visitCondition(cond Condition, fn func(Condition) error) error {
	if fn == nil {
		return nil
	}
	return fn(cond)
}

func visitValueExpr(expr ValueExpr, fn func(ValueExpr) error) error {
	if fn == nil {
		return nil
	}
	return fn(expr)
}

func visitPredicateExpr(expr PredicateExpr, fn func(PredicateExpr) error) error {
	if fn == nil {
		return nil
	}
	return fn(expr)
}

func isNilNode(node any) bool {
	if node == nil {
		return true
	}
	value := reflect.ValueOf(node)
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		return value.IsNil()
	}
	return false
}
