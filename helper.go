package gorbac

import "fmt"

// WalkHandler is a function defined by user to handle role
type WalkHandler[T comparable] func(Role[T], []T) error

// Walk passes each Role to WalkHandler
func Walk[T comparable](rbac RBAC[T], h WalkHandler[T]) (err error) {
	if h == nil {
		return
	}
	for _, id := range rbac.RoleIDs() {
		r, parents, err := rbac.Get(id)
		if err != nil {
			return err
		}
		if err := h(r, parents); err != nil {
			return err
		}
	}
	return
}

// InherCircle returns an error when detecting any circle inheritance.
func InherCircle[T comparable](rbac RBAC[T]) (err error) {
	skipped := make(map[T]struct{})
	var stack []T

	for _, id := range rbac.RoleIDs() {
		if err = dfs(rbac, id, skipped, stack); err != nil {
			break
		}
	}
	return err
}

var (
	ErrFoundCircle = fmt.Errorf("found circle")
)

// https://en.wikipedia.org/wiki/Depth-first_search
func dfs[T comparable](rbac RBAC[T], id T, skipped map[T]struct{},
	stack []T) error {
	if _, ok := skipped[id]; ok {
		return nil
	}
	for _, item := range stack {
		if item == id {
			return ErrFoundCircle
		}
	}
	parents, err := rbac.GetParents(id)
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		skipped[id] = empty
		return nil
	}
	stack = append(stack, id)
	for _, pid := range parents {
		if err := dfs(rbac, pid, skipped, stack); err != nil {
			return err
		}
	}
	return nil
}

// AnyGranted checks if any role has the permission.
func AnyGranted[T comparable](rbac RBAC[T], roles []T,
	permission Permission[T], assert AssertionFunc[T]) (ok bool) {
	for _, role := range roles {
		if rbac.IsGranted(role, permission, assert) {
			ok = true
			break
		}
	}
	return
}

// AllGranted checks if all roles have the permission.
func AllGranted[T comparable](rbac RBAC[T], roles []T,
	permission Permission[T], assert AssertionFunc[T]) (ok bool) {
	ok = true
	for _, role := range roles {
		if !rbac.IsGranted(role, permission, assert) {
			ok = false
			break
		}
	}
	return
}
