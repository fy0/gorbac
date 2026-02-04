package filter

// Condition represents a boolean expression derived from the CEL filter.
type Condition interface {
	isCondition()
}

// LogicalOperator enumerates the supported logical operators.
type LogicalOperator string

const (
	LogicalAnd LogicalOperator = "AND"
	LogicalOr  LogicalOperator = "OR"
)

// LogicalCondition composes two conditions with a logical operator.
type LogicalCondition struct {
	Operator LogicalOperator
	Left     Condition
	Right    Condition
}

func (*LogicalCondition) isCondition() {}

// NotCondition negates a child condition.
type NotCondition struct {
	Expr Condition
}

func (*NotCondition) isCondition() {}

// FieldPredicateCondition asserts that a boolean field evaluates to true.
type FieldPredicateCondition struct {
	Field string
}

func (*FieldPredicateCondition) isCondition() {}

// ComparisonOperator lists supported comparison operators.
type ComparisonOperator string

const (
	CompareEq  ComparisonOperator = "="
	CompareNeq ComparisonOperator = "!="
	CompareLt  ComparisonOperator = "<"
	CompareLte ComparisonOperator = "<="
	CompareGt  ComparisonOperator = ">"
	CompareGte ComparisonOperator = ">="
)

// ComparisonCondition represents a binary comparison.
type ComparisonCondition struct {
	Left     ValueExpr
	Operator ComparisonOperator
	Right    ValueExpr
}

func (*ComparisonCondition) isCondition() {}

// InCondition represents an IN predicate.
//
// Values can be:
//   - individual literals/params (e.g. id in [1,2,3])
//   - a single list param (e.g. id in project_ids)
type InCondition struct {
	Left   ValueExpr
	Values []ValueExpr
}

func (*InCondition) isCondition() {}

// ElementInCondition represents the CEL syntax `"value" in field`.
//
// This is primarily used for JSON list membership checks.
type ElementInCondition struct {
	Element ValueExpr
	Field   string
}

func (*ElementInCondition) isCondition() {}

// ContainsCondition models the <field>.contains(<value>) call.
type ContainsCondition struct {
	Field string
	Value ValueExpr
}

func (*ContainsCondition) isCondition() {}

// StartsWithCondition models the <field>.startsWith(<value>) call.
type StartsWithCondition struct {
	Field string
	Value ValueExpr
}

func (*StartsWithCondition) isCondition() {}

// EndsWithCondition models the <field>.endsWith(<value>) call.
type EndsWithCondition struct {
	Field string
	Value ValueExpr
}

func (*EndsWithCondition) isCondition() {}

// SQLPredicateCondition represents a custom predicate rendered as SQL.
//
// Instances are produced by `sql("<name>")` or `sql("<name>", [...])`.
// See SQLPredicate / WithSQLPredicate for registration.
type SQLPredicateCondition struct {
	Name string
	SQL  DialectSQL
	Args []ValueExpr
	Eval SQLPredicateEval
}

func (*SQLPredicateCondition) isCondition() {}

// ConstantCondition captures a literal boolean outcome.
type ConstantCondition struct {
	Value bool
}

func (*ConstantCondition) isCondition() {}

// ValueExpr models scalar expressions whose result feeds a comparison.
type ValueExpr interface {
	isValueExpr()
}

// FieldRef references a named schema field.
type FieldRef struct {
	Name string
}

func (*FieldRef) isValueExpr() {}

// ParamRef references a named CEL variable which is not a schema field.
//
// Values are supplied at runtime via bindings (SQL rendering) or vars
// (in-memory evaluation).
type ParamRef struct {
	Name string
}

func (*ParamRef) isValueExpr() {}

// LiteralValue holds a literal scalar.
type LiteralValue struct {
	Value any
}

func (*LiteralValue) isValueExpr() {}

// FunctionValue captures simple function calls like size(tags).
type FunctionValue struct {
	Name string
	Args []ValueExpr
}

func (*FunctionValue) isValueExpr() {}

// ListComprehensionCondition represents CEL macros like exists().
type ListComprehensionCondition struct {
	Kind      ComprehensionKind
	Field     string
	IterVar   string
	Predicate PredicateExpr
}

func (*ListComprehensionCondition) isCondition() {}

// ComprehensionKind enumerates the supported comprehension macros.
type ComprehensionKind string

const (
	ComprehensionExists ComprehensionKind = "exists"
)

// PredicateExpr represents predicates used in comprehensions.
type PredicateExpr interface {
	isPredicateExpr()
}

// StartsWithPredicate represents t.startsWith(prefix).
type StartsWithPredicate struct {
	Prefix ValueExpr
}

func (*StartsWithPredicate) isPredicateExpr() {}

// EndsWithPredicate represents t.endsWith(suffix).
type EndsWithPredicate struct {
	Suffix ValueExpr
}

func (*EndsWithPredicate) isPredicateExpr() {}

// ContainsPredicate represents t.contains(substring).
type ContainsPredicate struct {
	Substring ValueExpr
}

func (*ContainsPredicate) isPredicateExpr() {}
