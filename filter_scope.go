package gorbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/fy0/gorbac/v3/filter"
)

func collectRoleClosure[T comparable](ctx context.Context, rbac RBAC[T], roleID T) ([]Role[T], bool) {
	seen := make(map[T]struct{}, 8)
	closure := make([]Role[T], 0, 8)
	var dfs func(T)
	dfs = func(id T) {
		if _, ok := seen[id]; ok {
			return
		}
		role, err := rbac.Get(ctx, id)
		if err != nil {
			return
		}
		parents, err := rbac.GetParents(ctx, id)
		if err != nil {
			return
		}
		seen[id] = struct{}{}
		closure = append(closure, role)
		for _, parentID := range parents {
			dfs(parentID)
		}
	}
	dfs(roleID)
	if len(closure) == 0 {
		return nil, false
	}
	return closure, true
}

// FilterExprsForRoles returns combined CEL expressions per role.
//
// The required filter permissions are used only to select which filter
// expressions to compose; missing filters are treated as allow-all. Permission
// checks are expected to be handled separately.
func FilterExprsForRoles[T comparable](
	ctx context.Context,
	rbac RBAC[T],
	roles []T,
	requiredFilterPermissions []Permission[T],
) ([]string, error) {
	if len(requiredFilterPermissions) == 0 {
		return nil, fmt.Errorf("required filter permissions is empty")
	}

	exprs := make([]string, 0, len(roles))
	for _, roleID := range roles {
		expr, ok, err := filterExprForRole(ctx, rbac, roleID, requiredFilterPermissions)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		exprs = append(exprs, expr)
	}
	return exprs, nil
}

func filterExprForRole[T comparable](
	ctx context.Context,
	rbac RBAC[T],
	roleID T,
	requiredFilterPermissions []Permission[T],
) (string, bool, error) {
	closure, ok := collectRoleClosure(ctx, rbac, roleID)
	if !ok {
		return "", false, nil
	}
	exprsByPermission := make(map[T][]string)
	for _, role := range closure {
		for _, perm := range role.FilterPermissions(ctx) {
			f, ok := perm.(interface {
				CEL() (string, error)
			})
			if !ok {
				continue
			}
			expr, err := f.CEL()
			if err != nil {
				return "", false, err
			}
			normalized, err := normalizeExpr(expr)
			if err != nil {
				return "", false, err
			}
			exprsByPermission[perm.ID()] = append(exprsByPermission[perm.ID()], normalized)
		}
	}
	expr, err := buildSingleRoleExpr(exprsByPermission, requiredFilterPermissions)
	if err != nil {
		return "", false, err
	}
	return expr, true, nil
}

// NewFilterProgramFromCEL compiles CEL expressions into a single filter.Program.
//
// Expressions are OR-ed together. Optional `filter.EngineOption` values are
// forwarded to `filter.NewEngine`, including `filter.WithExtraFilterCEL(...)`
// which is AND-ed to the final condition.
func NewFilterProgramFromCEL(
	schema filter.Schema,
	exprs []string,
	engineOpts ...filter.EngineOption,
) (*filter.Program, error) {
	if len(exprs) == 0 {
		return filter.NewProgramFromCondition(schema, &filter.ConstantCondition{Value: false}), nil
	}

	engine, err := filter.NewEngine(schema, engineOpts...)
	if err != nil {
		return nil, err
	}

	roleConds := make([]filter.Condition, 0, len(exprs))
	for i, rawExpr := range exprs {
		expr, err := normalizeExpr(rawExpr)
		if err != nil {
			return nil, fmt.Errorf("expr %d: %w", i, err)
		}
		program, err := engine.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("expr %d: %w", i, err)
		}
		roleConds = append(roleConds, program.ConditionTree())
	}

	cond := filter.CondOr(roleConds...)

	if extra := strings.TrimSpace(engine.ExtraFilterCEL()); extra != "" {
		extraProg, err := engine.Compile(extra)
		if err != nil {
			return nil, err
		}
		cond = filter.CondAnd(cond, extraProg.ConditionTree())
	}

	return filter.NewProgramFromCondition(schema, cond), nil
}

func buildSingleRoleExpr[T comparable](
	exprsByPermission map[T][]string,
	requiredFilterPermissions []Permission[T],
) (string, error) {
	permExprs := make([]string, 0, len(requiredFilterPermissions))
	for _, permission := range requiredFilterPermissions {
		variants := exprsByPermission[permission.ID()]
		if len(variants) == 0 {
			permExprs = append(permExprs, "true")
			continue
		}
		permExprs = append(permExprs, orAllExprs(variants))
	}

	return andAllExprs(permExprs), nil
}

type combineRules struct {
	identity    string
	annihilator string
	joiner      string
}

func combineExprs(exprs []string, rules combineRules) string {
	if len(exprs) == 0 {
		return rules.identity
	}
	out := make([]string, 0, len(exprs))
	for _, expr := range exprs {
		expr = strings.TrimSpace(expr)
		if expr == "" || expr == rules.identity {
			continue
		}
		if expr == rules.annihilator {
			return rules.annihilator
		}
		out = append(out, wrapExpr(expr))
	}
	if len(out) == 0 {
		return rules.identity
	}
	if len(out) == 1 {
		return out[0]
	}
	return strings.Join(out, rules.joiner)
}

func andAllExprs(exprs []string) string {
	return combineExprs(exprs, combineRules{
		identity:    "true",
		annihilator: "false",
		joiner:      " && ",
	})
}

func orAllExprs(exprs []string) string {
	return combineExprs(exprs, combineRules{
		identity:    "false",
		annihilator: "true",
		joiner:      " || ",
	})
}

func wrapExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "true" || expr == "false" {
		return expr
	}
	if isWrappedExpr(expr) {
		return expr
	}
	return fmt.Sprintf("(%s)", expr)
}

func isWrappedExpr(expr string) bool {
	if !strings.HasPrefix(expr, "(") || !strings.HasSuffix(expr, ")") {
		return false
	}
	depth := 0
	for i, r := range expr {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(expr)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func normalizeExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", fmt.Errorf("filter expression is empty")
	}
	return expr, nil
}
