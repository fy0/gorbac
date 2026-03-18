// Package filter compiles CEL filters into a dialect-agnostic condition tree.
//
// The built-in SQL helpers such as Program.RenderSQL and RenderCondition are
// one renderer branch over that IR. Downstream integrations that target search
// engines, analyzers, or static checks should build on Program.Schema,
// Program.ConditionTree, WalkCondition, WalkValueExpr, and WalkPredicateExpr to
// implement their own renderer or analyzer without depending on SQL internals.
//
// A custom renderer typically looks like:
//
// 	prog, _ := engine.Compile(`title.contains(q) && visibility == "PUBLIC"`)
// 	schema := prog.Schema()
// 	cond := prog.ConditionTree()
// 	_ = schema
// 	_ = WalkCondition(cond, func(node Condition) error {
// 		// translate each IR node into your backend query representation
// 		return nil
// 	})
package filter
