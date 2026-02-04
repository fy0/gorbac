package filter

import (
	"fmt"
	"strings"
)

type renderer struct {
	schema             Schema
	dialect            DialectName
	placeholderOffset  int
	placeholderCounter int
	args               []any
	bindings           Bindings
}

type renderResult struct {
	sql           string
	trivial       bool
	unsatisfiable bool
}

func newRenderer(schema Schema, opts RenderOptions, bindings Bindings) *renderer {
	return &renderer{
		schema:            schema,
		dialect:           opts.Dialect,
		placeholderOffset: opts.PlaceholderOffset,
		bindings:          bindings,
	}
}

func (r *renderer) Render(cond Condition) (Statement, error) {
	result, err := r.renderCondition(cond)
	if err != nil {
		return Statement{}, err
	}

	switch {
	case result.unsatisfiable:
		return Statement{SQL: "1 = 0", Args: []any{}}, nil
	case result.trivial:
		return Statement{SQL: "", Args: []any{}}, nil
	default:
		args := r.args
		if args == nil {
			args = []any{}
		}
		return Statement{SQL: result.sql, Args: args}, nil
	}
}

func (r *renderer) renderCondition(cond Condition) (renderResult, error) {
	switch c := cond.(type) {
	case *LogicalCondition:
		return r.renderLogicalCondition(c)
	case *NotCondition:
		return r.renderNotCondition(c)
	case *FieldPredicateCondition:
		return r.renderFieldPredicate(c)
	case *ComparisonCondition:
		return r.renderComparison(c)
	case *InCondition:
		return r.renderInCondition(c)
	case *ElementInCondition:
		return r.renderElementInCondition(c)
	case *ContainsCondition:
		return r.renderContainsCondition(c)
	case *StartsWithCondition:
		return r.renderStartsWithCondition(c)
	case *EndsWithCondition:
		return r.renderEndsWithCondition(c)
	case *ListComprehensionCondition:
		return r.renderListComprehension(c)
	case *SQLPredicateCondition:
		return r.renderSQLPredicateCondition(c)
	case *ConstantCondition:
		if c.Value {
			return renderResult{trivial: true}, nil
		}
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	default:
		return renderResult{}, fmt.Errorf("unsupported condition type %T", c)
	}
}

func (r *renderer) renderSQLPredicateCondition(cond *SQLPredicateCondition) (renderResult, error) {
	template := cond.SQL.template(r.dialect)
	if strings.TrimSpace(template) == "" {
		return renderResult{}, fmt.Errorf("missing SQL template for predicate %q (dialect %s)", cond.Name, r.dialect)
	}

	sql, err := r.interpolateSQLColumns(template)
	if err != nil {
		return renderResult{}, err
	}

	placeholders := make([]string, 0, len(cond.Args))
	for _, arg := range cond.Args {
		raw, err := r.resolveValue(arg)
		if err != nil {
			return renderResult{}, err
		}
		if b, ok := raw.(bool); ok {
			placeholders = append(placeholders, r.addBoolArg(b))
			continue
		}
		placeholders = append(placeholders, r.addArg(raw))
	}

	sql, err = replaceSQLArgPlaceholders(sql, placeholders)
	if err != nil {
		return renderResult{}, fmt.Errorf("predicate %q: %w", cond.Name, err)
	}

	if strings.TrimSpace(sql) == "" {
		return renderResult{trivial: true}, nil
	}
	return renderResult{sql: sql}, nil
}

func (r *renderer) interpolateSQLColumns(template string) (string, error) {
	var out strings.Builder
	n := len(template)

	for i := 0; i < n; {
		if i+1 < n && template[i] == '{' && template[i+1] == '{' {
			end := strings.Index(template[i+2:], "}}")
			if end < 0 {
				return "", fmt.Errorf("unterminated {{...}} placeholder in SQL template")
			}
			name := strings.TrimSpace(template[i+2 : i+2+end])
			if name == "" {
				return "", fmt.Errorf("empty {{...}} placeholder in SQL template")
			}

			field, ok := r.schema.Field(name)
			if !ok {
				return "", fmt.Errorf("unknown field %q in SQL template placeholder", name)
			}
			if field.Kind == FieldKindVirtualAlias {
				resolved, ok := r.schema.ResolveAlias(name)
				if !ok {
					return "", fmt.Errorf("invalid alias %q in SQL template placeholder", name)
				}
				field = resolved
			}

			switch field.Kind {
			case "", FieldKindScalar, FieldKindBoolColumn:
				out.WriteString(field.columnExpr(r.dialect))
			default:
				return "", fmt.Errorf("field %q (kind %s) not supported in SQL template placeholders", name, field.Kind)
			}

			i += 2 + end + 2
			continue
		}

		out.WriteByte(template[i])
		i++
	}

	return out.String(), nil
}

func replaceSQLArgPlaceholders(template string, placeholders []string) (string, error) {
	if len(placeholders) == 0 {
		if strings.Contains(template, "?") {
			return "", fmt.Errorf("template contains '?' but no args were provided")
		}
		return template, nil
	}

	var out strings.Builder
	out.Grow(len(template) + len(placeholders)*2)

	argIdx := 0
	for i := 0; i < len(template); i++ {
		if template[i] == '?' {
			if argIdx >= len(placeholders) {
				return "", fmt.Errorf("template has more '?' than args (%d)", len(placeholders))
			}
			out.WriteString(placeholders[argIdx])
			argIdx++
			continue
		}
		out.WriteByte(template[i])
	}
	if argIdx != len(placeholders) {
		return "", fmt.Errorf("template has fewer '?' than args (%d)", len(placeholders))
	}
	return out.String(), nil
}

func (r *renderer) renderLogicalCondition(cond *LogicalCondition) (renderResult, error) {
	flattened := make([]Condition, 0, 4)
	flattenLogicalConditions(cond, cond.Operator, &flattened)

	rendered := make([]renderResult, 0, len(flattened))
	for _, child := range flattened {
		result, err := r.renderCondition(child)
		if err != nil {
			return renderResult{}, err
		}
		rendered = append(rendered, result)
	}

	switch cond.Operator {
	case LogicalAnd:
		return combineAndAll(rendered), nil
	case LogicalOr:
		return combineOrAll(rendered), nil
	default:
		return renderResult{}, fmt.Errorf("unsupported logical operator %s", cond.Operator)
	}
}

func flattenLogicalConditions(cond Condition, operator LogicalOperator, out *[]Condition) {
	if cond == nil {
		return
	}
	logical, ok := cond.(*LogicalCondition)
	if ok && logical.Operator == operator {
		flattenLogicalConditions(logical.Left, operator, out)
		flattenLogicalConditions(logical.Right, operator, out)
		return
	}
	*out = append(*out, cond)
}

func combineAndAll(conds []renderResult) renderResult {
	filtered := make([]renderResult, 0, len(conds))
	for _, cond := range conds {
		if cond.unsatisfiable {
			return renderResult{sql: "1 = 0", unsatisfiable: true}
		}
		if cond.trivial {
			continue
		}
		filtered = append(filtered, cond)
	}

	switch len(filtered) {
	case 0:
		return renderResult{trivial: true}
	case 1:
		return filtered[0]
	default:
		parts := make([]string, 0, len(filtered))
		for _, cond := range filtered {
			parts = append(parts, cond.sql)
		}
		return renderResult{sql: fmt.Sprintf("(%s)", strings.Join(parts, " AND "))}
	}
}

func combineOrAll(conds []renderResult) renderResult {
	filtered := make([]renderResult, 0, len(conds))
	for _, cond := range conds {
		if cond.trivial {
			return renderResult{trivial: true}
		}
		if cond.unsatisfiable {
			continue
		}
		filtered = append(filtered, cond)
	}

	switch len(filtered) {
	case 0:
		return renderResult{sql: "1 = 0", unsatisfiable: true}
	case 1:
		return filtered[0]
	default:
		parts := make([]string, 0, len(filtered))
		for _, cond := range filtered {
			parts = append(parts, cond.sql)
		}
		return renderResult{sql: fmt.Sprintf("(%s)", strings.Join(parts, " OR "))}
	}
}

func (r *renderer) renderNotCondition(cond *NotCondition) (renderResult, error) {
	child, err := r.renderCondition(cond.Expr)
	if err != nil {
		return renderResult{}, err
	}
	if child.trivial {
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	}
	if child.unsatisfiable {
		return renderResult{trivial: true}, nil
	}
	return renderResult{sql: fmt.Sprintf("NOT (%s)", child.sql)}, nil
}

func (r *renderer) renderFieldPredicate(cond *FieldPredicateCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}

	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := r.schema.ResolveAlias(cond.Field)
		if !ok {
			return renderResult{}, fmt.Errorf("invalid alias %q", cond.Field)
		}
		field = resolved
	}

	switch field.Kind {
	case FieldKindJSONBool:
		sql, err := r.jsonBoolPredicate(field)
		if err != nil {
			return renderResult{}, err
		}
		return renderResult{sql: sql}, nil
	default:
		if field.Type != FieldTypeBool {
			return renderResult{}, fmt.Errorf("field %q cannot be used as a predicate", cond.Field)
		}
		column := field.columnExpr(r.dialect)
		switch r.dialect {
		case DialectSQLite:
			return renderResult{sql: fmt.Sprintf("%s != 0", column)}, nil
		default:
			return renderResult{sql: fmt.Sprintf("%s IS TRUE", column)}, nil
		}
	}
}

func (r *renderer) renderComparison(cond *ComparisonCondition) (renderResult, error) {
	switch left := cond.Left.(type) {
	case *FieldRef:
		field, ok := r.schema.Field(left.Name)
		if !ok {
			return renderResult{}, fmt.Errorf("unknown field %q", left.Name)
		}
		if field.Kind == FieldKindVirtualAlias {
			resolved, ok := r.schema.ResolveAlias(left.Name)
			if !ok {
				return renderResult{}, fmt.Errorf("invalid alias %q", left.Name)
			}
			field = resolved
		}

		switch field.Kind {
		case FieldKindJSONBool:
			return r.renderJSONBoolComparison(field, cond.Operator, cond.Right)
		case FieldKindJSONList:
			return renderResult{}, fmt.Errorf("field %q does not support comparison", left.Name)
		default:
			return r.renderFieldComparison(field, cond.Operator, cond.Right)
		}
	case *FunctionValue:
		return r.renderFunctionComparison(left, cond.Operator, cond.Right)
	default:
		// Allow symmetry: `current_user_id == creator_id`.
		if rightField, ok := cond.Right.(*FieldRef); ok {
			op, err := invertComparisonOperator(cond.Operator)
			if err != nil {
				return renderResult{}, err
			}
			return r.renderComparison(&ComparisonCondition{
				Left:     rightField,
				Operator: op,
				Right:    cond.Left,
			})
		}
		// Allow symmetry: `0 < size(tags)`.
		if rightFn, ok := cond.Right.(*FunctionValue); ok {
			op, err := invertComparisonOperator(cond.Operator)
			if err != nil {
				return renderResult{}, err
			}
			return r.renderComparison(&ComparisonCondition{
				Left:     rightFn,
				Operator: op,
				Right:    cond.Left,
			})
		}

		// No column refs: fold to true/false using bindings only.
		vars := map[string]any(nil)
		if r.bindings != nil {
			vars = map[string]any(r.bindings)
		}
		ok, err := evalComparison(r.schema, cond, vars)
		if err != nil {
			return renderResult{}, err
		}
		if ok {
			return renderResult{trivial: true}, nil
		}
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	}
}

func (r *renderer) renderFieldComparison(field *Field, op ComparisonOperator, right ValueExpr) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	value, err := r.resolveValue(right)
	if err != nil {
		return renderResult{}, err
	}

	columnExpr := field.columnExpr(r.dialect)
	if value == nil {
		switch op {
		case CompareEq:
			return renderResult{sql: fmt.Sprintf("%s IS NULL", columnExpr)}, nil
		case CompareNeq:
			return renderResult{sql: fmt.Sprintf("%s IS NOT NULL", columnExpr)}, nil
		default:
			return renderResult{}, fmt.Errorf("operator %s not supported for null comparison", op)
		}
	}

	var placeholder string
	switch field.Type {
	case FieldTypeString:
		str, ok := value.(string)
		if !ok {
			return renderResult{}, fmt.Errorf("field %q expects string value", field.Name)
		}
		placeholder = r.addArg(str)
	case FieldTypeInt, FieldTypeTimestamp:
		num, err := toInt64(value)
		if err != nil {
			return renderResult{}, fmt.Errorf("field %q expects integer value: %w", field.Name, err)
		}
		placeholder = r.addArg(num)
	case FieldTypeBool:
		b, ok := value.(bool)
		if !ok {
			return renderResult{}, fmt.Errorf("field %q expects bool value", field.Name)
		}
		placeholder = r.addBoolArg(b)
	default:
		return renderResult{}, fmt.Errorf("unsupported field type %q for %s", field.Type, field.Name)
	}

	return renderResult{sql: fmt.Sprintf("%s %s %s", columnExpr, string(op), placeholder)}, nil
}

func (r *renderer) renderInCondition(cond *InCondition) (renderResult, error) {
	fieldRef, ok := cond.Left.(*FieldRef)
	if !ok {
		// No column refs: fold to true/false using bindings only.
		vars := map[string]any(nil)
		if r.bindings != nil {
			vars = map[string]any(r.bindings)
		}
		ok, err := evalIn(r.schema, cond, vars)
		if err != nil {
			return renderResult{}, err
		}
		if ok {
			return renderResult{trivial: true}, nil
		}
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	}

	field, ok := r.schema.Field(fieldRef.Name)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", fieldRef.Name)
	}

	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := r.schema.ResolveAlias(fieldRef.Name)
		if !ok {
			return renderResult{}, fmt.Errorf("invalid alias %q", fieldRef.Name)
		}
		if resolved.Kind == FieldKindJSONList {
			return r.renderAliasInList(fieldRef.Name, resolved, cond.Values)
		}
		return renderResult{}, fmt.Errorf("alias %q does not support IN()", fieldRef.Name)
	}

	if field.Kind == FieldKindJSONList {
		return renderResult{}, fmt.Errorf("field %q does not support IN(); use element-in (\"x\" in %s)", fieldRef.Name, fieldRef.Name)
	}

	flat := make([]any, 0, len(cond.Values))
	for _, v := range cond.Values {
		raw, err := r.resolveValue(v)
		if err != nil {
			return renderResult{}, err
		}
		if list, ok := toAnySlice(raw); ok {
			flat = append(flat, list...)
			continue
		}
		flat = append(flat, raw)
	}
	if len(flat) == 0 {
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	}

	placeholders := make([]string, 0, len(flat))
	for _, raw := range flat {
		if raw == nil {
			return renderResult{}, fmt.Errorf("field %q does not support IN() with null values", field.Name)
		}

		switch field.Type {
		case FieldTypeString:
			str, ok := raw.(string)
			if !ok {
				return renderResult{}, fmt.Errorf("field %q expects string values", field.Name)
			}
			placeholders = append(placeholders, r.addArg(str))
		case FieldTypeInt, FieldTypeTimestamp:
			num, err := toInt64(raw)
			if err != nil {
				return renderResult{}, fmt.Errorf("field %q expects integer values: %w", field.Name, err)
			}
			placeholders = append(placeholders, r.addArg(num))
		default:
			return renderResult{}, fmt.Errorf("field %q does not support IN()", field.Name)
		}
	}

	column := field.columnExpr(r.dialect)
	return renderResult{sql: fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ","))}, nil
}

func (r *renderer) renderAliasInList(aliasName string, field *Field, values []ValueExpr) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	flat := make([]any, 0, len(values))
	for _, v := range values {
		raw, err := r.resolveValue(v)
		if err != nil {
			return renderResult{}, err
		}
		if list, ok := toAnySlice(raw); ok {
			flat = append(flat, list...)
			continue
		}
		flat = append(flat, raw)
	}
	if len(flat) == 0 {
		return renderResult{sql: "1 = 0", unsatisfiable: true}, nil
	}

	conditions := make([]string, 0, len(flat))
	arrayExpr := jsonArrayExpr(r.dialect, field)
	hierarchical := aliasName == "tag"

	for _, raw := range flat {
		if raw == nil {
			return renderResult{}, fmt.Errorf("alias %q does not support IN() with null values", aliasName)
		}
		str, ok := raw.(string)
		if !ok {
			return renderResult{}, fmt.Errorf("alias %q expects string values", aliasName)
		}

		switch r.dialect {
		case DialectSQLite:
			exactMatch := fmt.Sprintf("%s LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s"%%`, str)))
			if hierarchical {
				prefixMatch := fmt.Sprintf("%s LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s/%%`, str)))
				conditions = append(conditions, fmt.Sprintf("(%s OR %s)", exactMatch, prefixMatch))
			} else {
				conditions = append(conditions, exactMatch)
			}
		case DialectMySQL:
			exactMatch := fmt.Sprintf("JSON_CONTAINS(%s, %s)", arrayExpr, r.addArg(fmt.Sprintf(`"%s"`, str)))
			if hierarchical {
				prefixMatch := fmt.Sprintf("%s LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s/%%`, str)))
				conditions = append(conditions, fmt.Sprintf("(%s OR %s)", exactMatch, prefixMatch))
			} else {
				conditions = append(conditions, exactMatch)
			}
		case DialectPostgres:
			exactMatch := fmt.Sprintf("%s @> jsonb_build_array(%s::json)", arrayExpr, r.addArg(fmt.Sprintf(`"%s"`, str)))
			if hierarchical {
				prefixMatch := fmt.Sprintf("(%s)::text LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s/%%`, str)))
				conditions = append(conditions, fmt.Sprintf("(%s OR %s)", exactMatch, prefixMatch))
			} else {
				conditions = append(conditions, exactMatch)
			}
		default:
			return renderResult{}, fmt.Errorf("unsupported dialect %s", r.dialect)
		}
	}

	if len(conditions) == 1 {
		return renderResult{sql: conditions[0]}, nil
	}
	return renderResult{sql: fmt.Sprintf("(%s)", strings.Join(conditions, " OR "))}, nil
}

func (r *renderer) renderElementInCondition(cond *ElementInCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := r.schema.ResolveAlias(cond.Field)
		if !ok {
			return renderResult{}, fmt.Errorf("invalid alias %q", cond.Field)
		}
		field = resolved
	}
	if field.Kind != FieldKindJSONList {
		return renderResult{}, fmt.Errorf("field %q is not a JSON list", cond.Field)
	}

	raw, err := r.resolveValue(cond.Element)
	if err != nil {
		return renderResult{}, err
	}
	str, ok := raw.(string)
	if !ok {
		return renderResult{}, fmt.Errorf("list membership requires string value, got %T", raw)
	}

	arrayExpr := jsonArrayExpr(r.dialect, field)
	switch r.dialect {
	case DialectSQLite:
		return renderResult{sql: fmt.Sprintf("%s LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s"%%`, str)))}, nil
	case DialectMySQL:
		return renderResult{sql: fmt.Sprintf("JSON_CONTAINS(%s, %s)", arrayExpr, r.addArg(fmt.Sprintf(`"%s"`, str)))}, nil
	case DialectPostgres:
		return renderResult{sql: fmt.Sprintf("%s @> jsonb_build_array(%s::json)", arrayExpr, r.addArg(fmt.Sprintf(`"%s"`, str)))}, nil
	default:
		return renderResult{}, fmt.Errorf("unsupported dialect %s", r.dialect)
	}
}

func (r *renderer) renderFunctionComparison(fn *FunctionValue, op ComparisonOperator, right ValueExpr) (renderResult, error) {
	if fn.Name != "size" {
		return renderResult{}, fmt.Errorf("unsupported function %s in comparison", fn.Name)
	}
	if len(fn.Args) != 1 {
		return renderResult{}, fmt.Errorf("size() expects one argument")
	}

	fieldArg, ok := fn.Args[0].(*FieldRef)
	if !ok {
		return renderResult{}, fmt.Errorf("size() argument must be a field")
	}

	field, ok := r.schema.Field(fieldArg.Name)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", fieldArg.Name)
	}
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := r.schema.ResolveAlias(fieldArg.Name)
		if !ok {
			return renderResult{}, fmt.Errorf("invalid alias %q", fieldArg.Name)
		}
		field = resolved
	}
	if field.Kind != FieldKindJSONList {
		return renderResult{}, fmt.Errorf("size() only supports json list fields, got %q", field.Name)
	}

	raw, err := r.resolveValue(right)
	if err != nil {
		return renderResult{}, err
	}
	num, err := toInt64(raw)
	if err != nil {
		return renderResult{}, fmt.Errorf("size() comparison expects integer value: %w", err)
	}

	expr := jsonArrayLengthExpr(r.dialect, field)
	placeholder := r.addArg(num)
	return renderResult{sql: fmt.Sprintf("%s %s %s", expr, string(op), placeholder)}, nil
}

func (r *renderer) renderJSONBoolComparison(field *Field, op ComparisonOperator, right ValueExpr) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	raw, err := r.resolveValue(right)
	if err != nil {
		return renderResult{}, err
	}
	value, ok := raw.(bool)
	if !ok {
		return renderResult{}, fmt.Errorf("field %q expects bool value", field.Name)
	}

	jsonExpr := jsonExtractExpr(r.dialect, field)
	switch r.dialect {
	case DialectSQLite:
		switch op {
		case CompareEq:
			if value {
				return renderResult{sql: fmt.Sprintf("%s IS TRUE", jsonExpr)}, nil
			}
			return renderResult{sql: fmt.Sprintf("NOT(%s IS TRUE)", jsonExpr)}, nil
		case CompareNeq:
			if value {
				return renderResult{sql: fmt.Sprintf("NOT(%s IS TRUE)", jsonExpr)}, nil
			}
			return renderResult{sql: fmt.Sprintf("%s IS TRUE", jsonExpr)}, nil
		default:
			return renderResult{}, fmt.Errorf("operator %s not supported for boolean JSON field", op)
		}
	case DialectMySQL:
		boolStr := "false"
		if value {
			boolStr = "true"
		}
		return renderResult{sql: fmt.Sprintf("%s %s CAST('%s' AS JSON)", jsonExpr, string(op), boolStr)}, nil
	case DialectPostgres:
		placeholder := r.addArg(value)
		return renderResult{sql: fmt.Sprintf("(%s)::boolean %s %s", jsonExpr, string(op), placeholder)}, nil
	default:
		return renderResult{}, fmt.Errorf("unsupported dialect %s", r.dialect)
	}
}

func (r *renderer) renderListComprehension(cond *ListComprehensionCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}
	if field.Kind == FieldKindVirtualAlias {
		resolved, ok := r.schema.ResolveAlias(cond.Field)
		if !ok {
			return renderResult{}, fmt.Errorf("invalid alias %q", cond.Field)
		}
		field = resolved
	}
	if field.Kind != FieldKindJSONList {
		return renderResult{}, fmt.Errorf("field %q is not a JSON list", cond.Field)
	}

	switch pred := cond.Predicate.(type) {
	case *StartsWithPredicate:
		prefix, err := r.resolveString(pred.Prefix)
		if err != nil {
			return renderResult{}, err
		}
		return r.renderJSONArrayStartsWith(field, prefix, cond.Kind)
	case *EndsWithPredicate:
		suffix, err := r.resolveString(pred.Suffix)
		if err != nil {
			return renderResult{}, err
		}
		return r.renderJSONArrayEndsWith(field, suffix, cond.Kind)
	case *ContainsPredicate:
		substring, err := r.resolveString(pred.Substring)
		if err != nil {
			return renderResult{}, err
		}
		return r.renderJSONArrayContains(field, substring, cond.Kind)
	default:
		return renderResult{}, fmt.Errorf("unsupported predicate type %T in comprehension", pred)
	}
}

func (r *renderer) resolveString(expr ValueExpr) (string, error) {
	raw, err := r.resolveValue(expr)
	if err != nil {
		return "", err
	}
	str, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("expected string value, got %T", raw)
	}
	return str, nil
}

func (r *renderer) renderJSONArrayStartsWith(field *Field, prefix string, _ ComprehensionKind) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	arrayExpr := jsonArrayExpr(r.dialect, field)

	switch r.dialect {
	case DialectSQLite, DialectMySQL:
		exactMatch := r.buildJSONArrayLike(arrayExpr, fmt.Sprintf(`%%"%s"%%`, prefix))
		prefixMatch := r.buildJSONArrayLike(arrayExpr, fmt.Sprintf(`%%"%s%%`, prefix))
		condition := fmt.Sprintf("(%s OR %s)", exactMatch, prefixMatch)
		return renderResult{sql: r.wrapWithNullCheck(arrayExpr, condition)}, nil
	case DialectPostgres:
		exactMatch := fmt.Sprintf("%s @> jsonb_build_array(%s::json)", arrayExpr, r.addArg(fmt.Sprintf(`"%s"`, prefix)))
		prefixMatch := fmt.Sprintf("(%s)::text LIKE %s", arrayExpr, r.addArg(fmt.Sprintf(`%%"%s%%`, prefix)))
		condition := fmt.Sprintf("(%s OR %s)", exactMatch, prefixMatch)
		return renderResult{sql: r.wrapWithNullCheck(arrayExpr, condition)}, nil
	default:
		return renderResult{}, fmt.Errorf("unsupported dialect %s", r.dialect)
	}
}

func (r *renderer) renderJSONArrayEndsWith(field *Field, suffix string, _ ComprehensionKind) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	arrayExpr := jsonArrayExpr(r.dialect, field)
	pattern := fmt.Sprintf(`%%%s"%%`, suffix)

	likeExpr := r.buildJSONArrayLike(arrayExpr, pattern)
	return renderResult{sql: r.wrapWithNullCheck(arrayExpr, likeExpr)}, nil
}

func (r *renderer) renderJSONArrayContains(field *Field, substring string, _ ComprehensionKind) (renderResult, error) {
	if field == nil {
		return renderResult{}, fmt.Errorf("field is nil")
	}
	arrayExpr := jsonArrayExpr(r.dialect, field)
	pattern := fmt.Sprintf(`%%%s%%`, substring)

	likeExpr := r.buildJSONArrayLike(arrayExpr, pattern)
	return renderResult{sql: r.wrapWithNullCheck(arrayExpr, likeExpr)}, nil
}

func (r *renderer) buildJSONArrayLike(arrayExpr, pattern string) string {
	switch r.dialect {
	case DialectSQLite, DialectMySQL:
		return fmt.Sprintf("%s LIKE %s", arrayExpr, r.addArg(pattern))
	case DialectPostgres:
		return fmt.Sprintf("(%s)::text LIKE %s", arrayExpr, r.addArg(pattern))
	default:
		return ""
	}
}

func (r *renderer) wrapWithNullCheck(arrayExpr, condition string) string {
	var nullCheck string
	switch r.dialect {
	case DialectSQLite:
		nullCheck = fmt.Sprintf("%s IS NOT NULL AND %s != '[]'", arrayExpr, arrayExpr)
	case DialectMySQL:
		nullCheck = fmt.Sprintf("%s IS NOT NULL AND JSON_LENGTH(%s) > 0", arrayExpr, arrayExpr)
	case DialectPostgres:
		nullCheck = fmt.Sprintf("%s IS NOT NULL AND jsonb_array_length(%s) > 0", arrayExpr, arrayExpr)
	default:
		return condition
	}
	return fmt.Sprintf("(%s AND %s)", condition, nullCheck)
}

func (r *renderer) jsonBoolPredicate(field *Field) (string, error) {
	if field == nil {
		return "", fmt.Errorf("field is nil")
	}
	expr := jsonExtractExpr(r.dialect, field)
	switch r.dialect {
	case DialectSQLite:
		return fmt.Sprintf("%s IS TRUE", expr), nil
	case DialectMySQL:
		return fmt.Sprintf("COALESCE(%s, CAST('false' AS JSON)) = CAST('true' AS JSON)", expr), nil
	case DialectPostgres:
		return fmt.Sprintf("(%s)::boolean IS TRUE", expr), nil
	default:
		return "", fmt.Errorf("unsupported dialect %s", r.dialect)
	}
}

func (r *renderer) renderContainsCondition(cond *ContainsCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}
	if field.Type != FieldTypeString {
		return renderResult{}, fmt.Errorf("field %q does not support contains()", cond.Field)
	}

	raw, err := r.resolveValue(cond.Value)
	if err != nil {
		return renderResult{}, err
	}
	needle, ok := raw.(string)
	if !ok {
		return renderResult{}, fmt.Errorf("contains() expects string value, got %T", raw)
	}
	if needle == "" {
		return renderResult{trivial: true}, nil
	}

	column := field.columnExpr(r.dialect)
	arg := fmt.Sprintf("%%%s%%", needle)
	switch r.dialect {
	case DialectPostgres:
		return renderResult{sql: fmt.Sprintf("%s ILIKE %s", column, r.addArg(arg))}, nil
	default:
		return renderResult{sql: fmt.Sprintf("%s LIKE %s", column, r.addArg(arg))}, nil
	}
}

func (r *renderer) renderStartsWithCondition(cond *StartsWithCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}
	if field.Type != FieldTypeString {
		return renderResult{}, fmt.Errorf("field %q does not support startsWith()", cond.Field)
	}

	raw, err := r.resolveValue(cond.Value)
	if err != nil {
		return renderResult{}, err
	}
	prefix, ok := raw.(string)
	if !ok {
		return renderResult{}, fmt.Errorf("startsWith() expects string value, got %T", raw)
	}
	if prefix == "" {
		return renderResult{trivial: true}, nil
	}

	column := field.columnExpr(r.dialect)
	arg := fmt.Sprintf("%s%%", prefix)
	switch r.dialect {
	case DialectPostgres:
		return renderResult{sql: fmt.Sprintf("%s ILIKE %s", column, r.addArg(arg))}, nil
	default:
		return renderResult{sql: fmt.Sprintf("%s LIKE %s", column, r.addArg(arg))}, nil
	}
}

func (r *renderer) renderEndsWithCondition(cond *EndsWithCondition) (renderResult, error) {
	field, ok := r.schema.Field(cond.Field)
	if !ok {
		return renderResult{}, fmt.Errorf("unknown field %q", cond.Field)
	}
	if field.Type != FieldTypeString {
		return renderResult{}, fmt.Errorf("field %q does not support endsWith()", cond.Field)
	}

	raw, err := r.resolveValue(cond.Value)
	if err != nil {
		return renderResult{}, err
	}
	suffix, ok := raw.(string)
	if !ok {
		return renderResult{}, fmt.Errorf("endsWith() expects string value, got %T", raw)
	}
	if suffix == "" {
		return renderResult{trivial: true}, nil
	}

	column := field.columnExpr(r.dialect)
	arg := fmt.Sprintf("%%%s", suffix)
	switch r.dialect {
	case DialectPostgres:
		return renderResult{sql: fmt.Sprintf("%s ILIKE %s", column, r.addArg(arg))}, nil
	default:
		return renderResult{sql: fmt.Sprintf("%s LIKE %s", column, r.addArg(arg))}, nil
	}
}

func combineAnd(left, right renderResult) renderResult {
	if left.unsatisfiable || right.unsatisfiable {
		return renderResult{sql: "1 = 0", unsatisfiable: true}
	}
	if left.trivial {
		return right
	}
	if right.trivial {
		return left
	}
	return renderResult{sql: fmt.Sprintf("(%s AND %s)", left.sql, right.sql)}
}

func combineOr(left, right renderResult) renderResult {
	if left.trivial || right.trivial {
		return renderResult{trivial: true}
	}
	if left.unsatisfiable {
		return right
	}
	if right.unsatisfiable {
		return left
	}
	return renderResult{sql: fmt.Sprintf("(%s OR %s)", left.sql, right.sql)}
}

func (r *renderer) addArg(value any) string {
	r.placeholderCounter++
	r.args = append(r.args, value)
	if r.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", r.placeholderOffset+r.placeholderCounter)
	}
	return "?"
}

func (r *renderer) addBoolArg(value bool) string {
	switch r.dialect {
	case DialectSQLite:
		if value {
			return r.addArg(int64(1))
		}
		return r.addArg(int64(0))
	default:
		return r.addArg(value)
	}
}

func (r *renderer) resolveValue(expr ValueExpr) (any, error) {
	switch v := expr.(type) {
	case *LiteralValue:
		return v.Value, nil
	case *ParamRef:
		if r.bindings == nil {
			return nil, fmt.Errorf("missing bindings for %q", v.Name)
		}
		value, ok := r.bindings[v.Name]
		if !ok {
			return nil, fmt.Errorf("missing binding value for %q", v.Name)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("expression must be a literal or param")
	}
}

func invertComparisonOperator(op ComparisonOperator) (ComparisonOperator, error) {
	switch op {
	case CompareEq:
		return CompareEq, nil
	case CompareNeq:
		return CompareNeq, nil
	case CompareLt:
		return CompareGt, nil
	case CompareLte:
		return CompareGte, nil
	case CompareGt:
		return CompareLt, nil
	case CompareGte:
		return CompareLte, nil
	default:
		return "", fmt.Errorf("unsupported comparison operator %q", op)
	}
}

func qualifyColumn(d DialectName, col Column) string {
	switch d {
	case DialectPostgres:
		return fmt.Sprintf("%s.%s", col.Table, col.Name)
	default:
		return fmt.Sprintf("`%s`.`%s`", col.Table, col.Name)
	}
}

func jsonPath(field *Field) string {
	if field == nil {
		return ""
	}
	return "$." + strings.Join(field.JSONPath, ".")
}

func jsonExtractExpr(d DialectName, field *Field) string {
	if field == nil {
		return ""
	}
	column := qualifyColumn(d, field.Column)
	switch d {
	case DialectSQLite, DialectMySQL:
		return fmt.Sprintf("JSON_EXTRACT(%s, '%s')", column, jsonPath(field))
	case DialectPostgres:
		return buildPostgresJSONAccessor(column, field.JSONPath, true)
	default:
		return ""
	}
}

func jsonArrayExpr(d DialectName, field *Field) string {
	if field == nil {
		return ""
	}
	column := qualifyColumn(d, field.Column)
	switch d {
	case DialectSQLite, DialectMySQL:
		return fmt.Sprintf("JSON_EXTRACT(%s, '%s')", column, jsonPath(field))
	case DialectPostgres:
		return buildPostgresJSONAccessor(column, field.JSONPath, false)
	default:
		return ""
	}
}

func jsonArrayLengthExpr(d DialectName, field *Field) string {
	if field == nil {
		return ""
	}
	arrayExpr := jsonArrayExpr(d, field)
	switch d {
	case DialectSQLite:
		return fmt.Sprintf("JSON_ARRAY_LENGTH(COALESCE(%s, JSON_ARRAY()))", arrayExpr)
	case DialectMySQL:
		return fmt.Sprintf("JSON_LENGTH(COALESCE(%s, JSON_ARRAY()))", arrayExpr)
	case DialectPostgres:
		return fmt.Sprintf("jsonb_array_length(COALESCE(%s, '[]'::jsonb))", arrayExpr)
	default:
		return ""
	}
}

func buildPostgresJSONAccessor(base string, path []string, terminalText bool) string {
	expr := base
	for idx, part := range path {
		if idx == len(path)-1 && terminalText {
			expr = fmt.Sprintf("%s->>'%s'", expr, part)
		} else {
			expr = fmt.Sprintf("%s->'%s'", expr, part)
		}
	}
	return expr
}
