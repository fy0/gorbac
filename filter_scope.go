package gorbac

import (
	"fmt"

	"github.com/fy0/gorbac/v3/filter"
)

type permissionClosureCache[T comparable] struct {
	rbac *RBAC[T]

	// roleClosure caches role IDs reachable from a role (self + parents), de-duped.
	//
	// This avoids repeatedly walking the inheritance graph and prevents duplicated
	// work/variants when multiple parents share ancestors.
	roleClosure map[T][]T

	// directPermissions caches only the permissions directly assigned to a role.
	directPermissions map[T][]Permission[T]

	// allPermissions caches all permissions a role has (direct + inherited).
	allPermissions map[T][]Permission[T]
}

func newPermissionClosureCache[T comparable](rbac *RBAC[T]) *permissionClosureCache[T] {
	return &permissionClosureCache[T]{
		rbac:              rbac,
		roleClosure:       make(map[T][]T),
		directPermissions: make(map[T][]Permission[T]),
		allPermissions:    make(map[T][]Permission[T]),
	}
}

func (c *permissionClosureCache[T]) permissions(roleID T) []Permission[T] {
	if perms, ok := c.allPermissions[roleID]; ok {
		return perms
	}

	// Use a per-call stack guard to tolerate cyclic inheritance.
	visiting := make(map[T]struct{}, 8)
	closure, _ := c.roleClosureInternal(roleID, visiting)
	if len(closure) == 0 {
		c.allPermissions[roleID] = nil
		return nil
	}

	merged := make([]Permission[T], 0, 8)
	for _, id := range closure {
		merged = append(merged, c.directPermissions[id]...)
	}
	c.allPermissions[roleID] = merged
	return merged
}

func (c *permissionClosureCache[T]) roleClosureInternal(roleID T, visiting map[T]struct{}) ([]T, bool) {
	if closure, ok := c.roleClosure[roleID]; ok {
		return closure, true
	}

	// Cycles are treated as "already visited", similar to the previous
	// collectMatchingPermissions() implementation.
	if _, ok := visiting[roleID]; ok {
		return nil, true
	}
	visiting[roleID] = struct{}{}
	defer delete(visiting, roleID)

	role, parents, err := c.rbac.Get(roleID)
	if err != nil {
		// Keep legacy behavior: missing role IDs behave like "no permissions".
		c.roleClosure[roleID] = nil
		c.directPermissions[roleID] = nil
		return nil, false
	}

	c.directPermissions[roleID] = role.Permissions()

	closure := make([]T, 0, 1+len(parents))
	closure = append(closure, roleID)
	seen := map[T]struct{}{roleID: {}}
	for _, parentID := range parents {
		parentClosure, _ := c.roleClosureInternal(parentID, visiting)
		for _, id := range parentClosure {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			closure = append(closure, id)
		}
	}

	c.roleClosure[roleID] = closure
	return closure, true
}

func matchPermissions[T comparable](all []Permission[T], requested Permission[T]) []Permission[T] {
	if len(all) == 0 {
		return nil
	}
	matching := make([]Permission[T], 0, 4)
	for _, assigned := range all {
		if assigned.Match(requested) {
			matching = append(matching, assigned)
		}
	}
	return matching
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

	cache := newPermissionClosureCache(rbac)

	roleConds := make([]filter.Condition, 0, len(roles))
	for _, roleID := range roles {
		rolePerms := cache.permissions(roleID)
		roleCond, ok, err := buildSingleRoleCondition(engine, rolePerms, permissions)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		roleConds = append(roleConds, roleCond)
	}

	if len(roleConds) == 0 {
		return filter.NewProgramFromCondition(schema, &filter.ConstantCondition{Value: false}), nil
	}
	return filter.NewProgramFromCondition(schema, orAll(roleConds)), nil
}

func buildSingleRoleCondition[T comparable](
	engine *filter.Engine,
	rolePermissions []Permission[T],
	permissions []Permission[T],
) (filter.Condition, bool, error) {
	buckets := bucketPermissionsByMatchKind(rolePermissions)

	permConds := make([]filter.Condition, 0, len(permissions))
	for _, permission := range permissions {
		matching := buckets.match(permission)
		if len(matching) == 0 {
			return nil, false, nil
		}

		variants, err := collectPermissionVariantConditions(engine, matching)
		if err != nil {
			return nil, false, err
		}
		permConds = append(permConds, orAll(variants))
	}

	return andAll(permConds), true, nil
}

type permissionBuckets[T comparable] struct {
	exactByID map[T][]Permission[T]
	nonExact  []Permission[T]
}

func bucketPermissionsByMatchKind[T comparable](all []Permission[T]) permissionBuckets[T] {
	if len(all) == 0 {
		return permissionBuckets[T]{exactByID: make(map[T][]Permission[T])}
	}

	buckets := permissionBuckets[T]{
		exactByID: make(map[T][]Permission[T], len(all)),
		nonExact:  make([]Permission[T], 0, len(all)),
	}
	for _, p := range all {
		if isExactMatchOnlyPermission(p) {
			id := p.ID()
			buckets.exactByID[id] = append(buckets.exactByID[id], p)
			continue
		}
		buckets.nonExact = append(buckets.nonExact, p)
	}
	return buckets
}

func isExactMatchOnlyPermission[T comparable](p Permission[T]) bool {
	switch p.(type) {
	case StdPermission[T], *StdPermission[T]:
		return true
	case FilterPermission[T], *FilterPermission[T]:
		return true
	default:
		return false
	}
}

func (b permissionBuckets[T]) match(requested Permission[T]) []Permission[T] {
	matching := make([]Permission[T], 0, 4)

	if b.exactByID != nil {
		if perms := b.exactByID[requested.ID()]; len(perms) != 0 {
			matching = append(matching, perms...)
		}
	}

	for _, assigned := range b.nonExact {
		if assigned.Match(requested) {
			matching = append(matching, assigned)
		}
	}

	return matching
}

func collectPermissionVariantConditions[T comparable](
	engine *filter.Engine,
	matching []Permission[T],
) ([]filter.Condition, error) {
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
