package gorbac

import (
	"fmt"

	"github.com/mikespook/gorbac/v3/filter"
)

// filterRolesByPermissions returns roles that satisfy all required permissions.
//
// 角色必须同时拥有这些权限，才参与数据范围过滤
func filterRolesByPermissions[T comparable](rbac *RBAC[T], roles []T, permissions []Permission[T]) []T {
	if len(permissions) == 0 {
		return roles
	}

	filtered := make([]T, 0, len(roles))
	for _, roleID := range roles {
		matchesAll := true
		for _, permission := range permissions {
			if !rbac.IsGranted(roleID, permission, nil) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			filtered = append(filtered, roleID)
		}
	}

	return filtered
}

// collectMatchingPermissions returns all permissions that match the requested permission,
// including those inherited from parent roles.
func collectMatchingPermissions[T comparable](rbac *RBAC[T], roleID T, requested Permission[T]) ([]Permission[T], error) {
	visited := make(map[T]struct{})
	result := make([]Permission[T], 0, 4)

	var walk func(id T) error
	walk = func(id T) error {
		if _, ok := visited[id]; ok {
			return nil
		}
		visited[id] = struct{}{}

		role, parents, err := rbac.Get(id)
		if err != nil {
			return err
		}

		for _, assigned := range role.Permissions() {
			if assigned.Match(requested) {
				result = append(result, assigned)
			}
		}

		for _, parentID := range parents {
			if err := walk(parentID); err != nil {
				return err
			}
		}

		return nil
	}

	if err := walk(roleID); err != nil {
		return nil, err
	}
	return result, nil
}

// NewFilterProgram builds a single filter.Program that represents the union of
// all accessible rows across the provided roles.
//
// Semantics:
//   - Filter within a role: AND across `permissions`
//   - Multiple matches for one permission (e.g. inherited): OR
//   - Across roles: OR
//
// The returned program can be:
//   - rendered into SQL via `program.RenderSQL(bindings, opts)`
//   - evaluated in-memory via `program.IsGranted(vars, opts)`
func NewFilterProgram[T comparable](
	rbac *RBAC[T],
	roles []T,
	permissions []Permission[T],
	schema filter.Schema,
	engineOpts ...filter.EngineOption,
) (*filter.Program, error) {
	if len(permissions) == 0 {
		return nil, fmt.Errorf("permissions is empty")
	}

	engine, err := filter.NewEngine(schema, engineOpts...)
	if err != nil {
		return nil, err
	}

	filteredRoles := filterRolesByPermissions(rbac, roles, permissions)
	if len(filteredRoles) == 0 {
		return filter.NewProgramFromCondition(schema, &filter.ConstantCondition{Value: false}), nil
	}

	roleConds := make([]filter.Condition, 0, len(filteredRoles))
	for _, roleID := range filteredRoles {
		roleCond, err := buildSingleRoleCondition(engine, rbac, roleID, permissions)
		if err != nil {
			return nil, err
		}
		roleConds = append(roleConds, roleCond)
	}

	return filter.NewProgramFromCondition(schema, orAll(roleConds)), nil
}

func buildSingleRoleCondition[T comparable](
	engine *filter.Engine,
	rbac *RBAC[T],
	roleID T,
	permissions []Permission[T],
) (filter.Condition, error) {
	permConds := make([]filter.Condition, 0, len(permissions))
	for _, permission := range permissions {
		variants, err := collectPermissionVariantConditions(engine, rbac, roleID, permission)
		if err != nil {
			return nil, err
		}
		permConds = append(permConds, orAll(variants))
	}

	return andAll(permConds), nil
}

func collectPermissionVariantConditions[T comparable](
	engine *filter.Engine,
	rbac *RBAC[T],
	roleID T,
	requested Permission[T],
) ([]filter.Condition, error) {
	matching, err := collectMatchingPermissions(rbac, roleID, requested)
	if err != nil {
		return nil, err
	}
	if len(matching) == 0 {
		return []filter.Condition{&filter.ConstantCondition{Value: false}}, nil
	}

	variants := make([]filter.Condition, 0, len(matching))
	for _, assigned := range matching {
		if f, ok := assigned.(interface {
			CEL() (string, error)
		}); ok {
			expr, err := f.CEL()
			if err != nil {
				return nil, err
			}
			program, err := engine.Compile(expr)
			if err != nil {
				return nil, err
			}
			variants = append(variants, program.ConditionTree())
			continue
		}

		// Permissions without attached filters are treated as allow-all.
		variants = append(variants, &filter.ConstantCondition{Value: true})
	}

	return variants, nil
}

func andAll(conds []filter.Condition) filter.Condition {
	if len(conds) == 0 {
		return &filter.ConstantCondition{Value: true}
	}
	out := conds[0]
	for i := 1; i < len(conds); i++ {
		out = &filter.LogicalCondition{
			Operator: filter.LogicalAnd,
			Left:     out,
			Right:    conds[i],
		}
	}
	return out
}

func orAll(conds []filter.Condition) filter.Condition {
	if len(conds) == 0 {
		return &filter.ConstantCondition{Value: false}
	}
	out := conds[0]
	for i := 1; i < len(conds); i++ {
		out = &filter.LogicalCondition{
			Operator: filter.LogicalOr,
			Left:     out,
			Right:    conds[i],
		}
	}
	return out
}
