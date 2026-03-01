package gorbac

import (
	"context"
	"fmt"
)

// WalkHandler is a function defined by user to handle role
type WalkHandler[T comparable] func(Role[T], []T) error

// Walk passes each Role to WalkHandler
func Walk[T comparable](ctx context.Context, rbac RBAC[T], h WalkHandler[T]) (err error) {
	if h == nil {
		return
	}
	for _, id := range rbac.RoleIDs(ctx) {
		r, err := rbac.Get(ctx, id)
		if err != nil {
			return err
		}
		parents, err := rbac.GetParents(ctx, id)
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
func InherCircle[T comparable](ctx context.Context, rbac RBAC[T]) (err error) {
	skipped := make(map[T]struct{})
	var stack []T

	for _, id := range rbac.RoleIDs(ctx) {
		if err = dfs(ctx, rbac, id, skipped, stack); err != nil {
			break
		}
	}
	return err
}

var (
	ErrFoundCircle = fmt.Errorf("found circle")
)

// https://en.wikipedia.org/wiki/Depth-first_search
func dfs[T comparable](ctx context.Context, rbac RBAC[T], id T, skipped map[T]struct{},
	stack []T) error {
	if _, ok := skipped[id]; ok {
		return nil
	}
	for _, item := range stack {
		if item == id {
			return ErrFoundCircle
		}
	}
	parents, err := rbac.GetParents(ctx, id)
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		skipped[id] = empty
		return nil
	}
	stack = append(stack, id)
	for _, pid := range parents {
		if err := dfs(ctx, rbac, pid, skipped, stack); err != nil {
			return err
		}
	}
	return nil
}

// AnyGranted checks whether the role set grants any specified permission.
func AnyGranted[T comparable](ctx context.Context, rbac RBAC[T], roles []T,
	permissions ...Permission[T]) (ok bool) {
	if len(roles) == 0 || len(permissions) == 0 {
		return false
	}
	for _, permission := range permissions {
		for _, role := range roles {
			if rbac.IsGranted(ctx, role, permission) {
				return true
			}
		}
	}
	return false
}

// AllGranted checks whether the role set grants all specified permissions.
func AllGranted[T comparable](ctx context.Context, rbac RBAC[T], roles []T,
	permissions ...Permission[T]) (ok bool) {
	if len(roles) == 0 || len(permissions) == 0 {
		return false
	}
	for _, permission := range permissions {
		granted := false
		for _, role := range roles {
			if rbac.IsGranted(ctx, role, permission) {
				granted = true
				break
			}
		}
		if !granted {
			return false
		}
	}
	return true
}
