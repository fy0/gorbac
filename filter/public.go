package filter

// RenderCondition renders a pre-built condition tree into a SQL fragment.
//
// This is useful when you need to compose multiple compiled filters together
// (e.g. OR across roles) before rendering once.
func RenderCondition(schema Schema, cond Condition, bindings Bindings, opts RenderOptions) (Statement, error) {
	r := newRenderer(schema, opts, bindings)
	return r.Render(cond)
}

// NewProgramFromCondition wraps an already-built condition tree as a Program.
//
// This is useful when conditions are created programmatically (without CEL parsing),
// but you still want a single object capable of rendering SQL and evaluating objects.
func NewProgramFromCondition(schema Schema, cond Condition) *Program {
	if cond == nil {
		cond = &ConstantCondition{Value: true}
	}
	return &Program{
		schema:    schema,
		condition: cond,
	}
}
