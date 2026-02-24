package filter

// AppendConditions compiles the provided filters and appends the resulting SQL fragments and args.
// func AppendConditions(engine *Engine, filters []string, dialect DialectName, where *[]string, args *[]any) error {
// 	for _, filterStr := range filters {
// 		stmt, err := engine.CompileToStatement(filterStr, nil, RenderOptions{
// 			Dialect:           dialect,
// 			PlaceholderOffset: len(*args),
// 		})
// 		if err != nil {
// 			return err
// 		}
// 		if stmt.SQL == "" {
// 			continue
// 		}
// 		*where = append(*where, fmt.Sprintf("(%s)", stmt.SQL))
// 		*args = append(*args, stmt.Args...)
// 	}
// 	return nil
// }

// And combines all conditions with logical AND.
func CondAnd(c ...Condition) Condition {
	if len(c) == 0 {
		return &ConstantCondition{Value: true}
	}
	out := c[0]
	for i := 1; i < len(c); i++ {
		out = &LogicalCondition{
			Operator: LogicalAnd,
			Left:     out,
			Right:    c[i],
		}
	}
	return out
}

// Or combines all conditions with logical OR.
func CondOr(c ...Condition) Condition {
	if len(c) == 0 {
		return &ConstantCondition{Value: false}
	}
	out := c[0]
	for i := 1; i < len(c); i++ {
		out = &LogicalCondition{
			Operator: LogicalOr,
			Left:     out,
			Right:    c[i],
		}
	}
	return out
}
