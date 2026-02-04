package filter

import (
	"fmt"
)

// AppendConditions compiles the provided filters and appends the resulting SQL fragments and args.
func AppendConditions(engine *Engine, filters []string, dialect DialectName, where *[]string, args *[]any) error {
	for _, filterStr := range filters {
		stmt, err := engine.CompileToStatement(filterStr, nil, RenderOptions{
			Dialect:           dialect,
			PlaceholderOffset: len(*args),
		})
		if err != nil {
			return err
		}
		if stmt.SQL == "" {
			continue
		}
		*where = append(*where, fmt.Sprintf("(%s)", stmt.SQL))
		*args = append(*args, stmt.Args...)
	}
	return nil
}
