package filter

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
)

// CompileHook can rewrite or replace the compiled condition tree.
//
// Hooks run after CEL parsing/type-checking and IR building, but before the
// resulting Program is returned.
//
// Returning (nil, nil) keeps the current condition unchanged.
type CompileHook func(schema Schema, filter string, ast *cel.Ast, cond Condition) (Condition, error)

type engineConfig struct {
	envOptions    []cel.EnvOption
	compileHook   []CompileHook
	sqlPredicates map[string]SQLPredicate
}

// EngineOption customizes Engine construction.
type EngineOption func(*engineConfig)

// WithEnvOptions appends additional CEL environment options when creating the Engine.
//
// This is the intended "extension hook" for callers to register custom CEL
// macros, functions, declarations, etc.
func WithEnvOptions(opts ...cel.EnvOption) EngineOption {
	return func(cfg *engineConfig) {
		cfg.envOptions = append(cfg.envOptions, opts...)
	}
}

// WithMacros is a convenience helper for registering custom CEL macros.
func WithMacros(macros ...cel.Macro) EngineOption {
	if len(macros) == 0 {
		return func(*engineConfig) {}
	}
	return WithEnvOptions(cel.Macros(macros...))
}

// WithCompileHook appends a post-compile hook which can rewrite the compiled condition tree.
func WithCompileHook(hook CompileHook) EngineOption {
	return func(cfg *engineConfig) {
		if hook == nil {
			return
		}
		cfg.compileHook = append(cfg.compileHook, hook)
	}
}

// Engine parses CEL filters into a dialect-agnostic condition tree.
type Engine struct {
	schema Schema
	env    *cel.Env

	compileHooks  []CompileHook
	sqlPredicates map[string]SQLPredicate
}

// NewEngine builds a new Engine for the provided schema.
func NewEngine(schema Schema, opts ...EngineOption) (*Engine, error) {
	cfg := &engineConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(cfg)
	}

	envOpts := make([]cel.EnvOption, 0, len(schema.EnvOptions)+len(cfg.envOptions))
	envOpts = append(envOpts, schema.EnvOptions...)
	envOpts = append(envOpts, cfg.envOptions...)
	if len(cfg.sqlPredicates) != 0 {
		envOpts = append(envOpts, SQLFunction)
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	return &Engine{
		schema:        schema,
		env:           env,
		compileHooks:  cfg.compileHook,
		sqlPredicates: cfg.sqlPredicates,
	}, nil
}

// Program stores a compiled filter condition.
type Program struct {
	schema    Schema
	condition Condition
}

// ConditionTree exposes the underlying condition tree.
func (p *Program) ConditionTree() Condition {
	return p.condition
}

// IsGranted evaluates the compiled condition tree against an object var map.
func (p *Program) IsGranted(vars map[string]any, opts EvalOptions) (bool, error) {
	return EvaluateCondition(p.schema, p.condition, vars, opts)
}

// Compile parses the filter string into an executable program.
func (e *Engine) Compile(filter string) (*Program, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, fmt.Errorf("filter expression is empty")
	}

	ast, issues := e.env.Compile(filter)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile filter: %w", issues.Err())
	}
	parsed, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AST: %w", err)
	}

	cond, err := buildCondition(parsed.GetExpr(), e.schema, e.sqlPredicates)
	if err != nil {
		return nil, err
	}

	for _, hook := range e.compileHooks {
		next, err := hook(e.schema, filter, ast, cond)
		if err != nil {
			return nil, err
		}
		if next != nil {
			cond = next
		}
	}

	return &Program{
		schema:    e.schema,
		condition: cond,
	}, nil
}

// IsGranted executes the filter as a CEL program and expects a boolean outcome.
//
// The vars input is a `map[string]any` holding values for schema-defined fields.
func (e *Engine) IsGranted(filter string, vars map[string]any) (bool, error) {
	program, err := e.Compile(filter)
	if err != nil {
		return false, err
	}
	return program.IsGranted(vars, EvalOptions{})
}

// CompileToStatement compiles and renders the filter in a single step.
func (e *Engine) CompileToStatement(filter string, bindings Bindings, opts RenderOptions) (Statement, error) {
	program, err := e.Compile(filter)
	if err != nil {
		return Statement{}, err
	}
	return program.RenderSQL(bindings, opts)
}

// RenderOptions configure SQL rendering.
type RenderOptions struct {
	Dialect           DialectName
	PlaceholderOffset int
	// TableAliases maps schema column table names to SQL qualifiers (usually aliases).
	//
	// This is useful when the schema was defined against a concrete table name but
	// the actual query uses a table alias:
	//
	//   schema column: {Table: "project", Name: "id"}
	//   query: FROM project p
	//   opts: TableAliases{"project": "p"} -> renders "p.id"
	//
	// A mapped empty string disables qualification for that table.
	TableAliases map[string]string
	// OmitTableQualifier disables table qualification for all columns, rendering
	// "id" instead of "t.id".
	//
	// This is useful when composing fragments into queries that use different
	// aliases (or no alias).
	OmitTableQualifier bool
}

// Statement contains the rendered SQL fragment and its args.
type Statement struct {
	SQL  string
	Args []any
	// NamedArgs is populated when rendering with DialectPostgresNamedArgs.
	//
	// It is intended to be passed to pgx as `pgx.NamedArgs(stmt.NamedArgs)`:
	// `conn.Query(ctx, "SELECT ... WHERE "+stmt.SQL, pgx.NamedArgs(stmt.NamedArgs))`
	NamedArgs Bindings
}

// RenderSQL converts the program into a dialect-specific SQL fragment.
func (p *Program) RenderSQL(bindings Bindings, opts RenderOptions) (Statement, error) {
	renderer := newRenderer(p.schema, opts, bindings)
	return renderer.Render(p.condition)
}
