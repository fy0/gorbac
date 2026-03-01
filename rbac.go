/*
Package gorbac provides a lightweight role-based access
control implementation in Golang.

For the purposes of this package:

  - an identity has one or more roles.
  - a role requests access to a permission.
  - a permission is given to a role.

Thus, RBAC has the following model:

  - many to many relationship between identities and roles.
  - many to many relationship between roles and permissions.
  - roles can have parent roles.
*/
package gorbac

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrRoleNotExist occurred if a role cann't be found
	ErrRoleNotExist = errors.New("Role does not exist")
	// ErrRoleExist occurred if a role shouldn't be found
	ErrRoleExist = errors.New("Role has already existed")
	empty        = struct{}{}
)

// RBAC defines the role-based access control contract.
type RBAC[T comparable] interface {
	Add(ctx context.Context, role Role[T]) error
	Remove(ctx context.Context, id T) error
	Get(ctx context.Context, id T) (Role[T], error)
	RoleIDs(ctx context.Context) []T
	SetParents(ctx context.Context, id T, parents ...T) error
	GetParents(ctx context.Context, id T) ([]T, error)
	RemoveParents(ctx context.Context, id T, parents ...T) error
	IsGranted(ctx context.Context, roleID T, permission Permission[T]) bool
}

// StdRBAC object, in most cases it should be used as a singleton.
type StdRBAC[T comparable] struct {
	mutex   sync.RWMutex
	roles   Roles[T]
	parents map[T]map[T]struct{}
}

// New returns a StdRBAC structure.
// The default role structure will be used.
func New[T comparable]() *StdRBAC[T] {
	return &StdRBAC[T]{
		roles:   make(Roles[T]),
		parents: make(map[T]map[T]struct{}),
	}
}

// SetParents bind `parents` to the role `id`.
// If the role or any of parents is not existing,
// an error will be returned.
func (rbac *StdRBAC[T]) SetParents(_ context.Context, id T, parents ...T) error {
	rbac.mutex.Lock()
	defer rbac.mutex.Unlock()
	if _, ok := rbac.roles[id]; !ok {
		return ErrRoleNotExist
	}
	for _, parent := range parents {
		if _, ok := rbac.roles[parent]; !ok {
			return ErrRoleNotExist
		}
	}
	if _, ok := rbac.parents[id]; !ok {
		rbac.parents[id] = make(map[T]struct{})
	}
	for _, parent := range parents {
		rbac.parents[id][parent] = empty
	}
	return nil
}

// GetParents return `parents` of the role `id`.
// If the role is not existing, an error will be returned.
// Or the role doesn't have any parents,
// a nil slice will be returned.
func (rbac *StdRBAC[T]) GetParents(_ context.Context, id T) ([]T, error) {
	rbac.mutex.Lock()
	defer rbac.mutex.Unlock()
	if _, ok := rbac.roles[id]; !ok {
		return nil, ErrRoleNotExist
	}
	ids, ok := rbac.parents[id]
	if !ok {
		return nil, nil
	}
	var parents []T
	for parent := range ids {
		parents = append(parents, parent)
	}
	return parents, nil
}

// RemoveParents unbind `parents` from the role `id`.
// If the role or any parent is not existing,
// an error will be returned.
func (rbac *StdRBAC[T]) RemoveParents(_ context.Context, id T, parents ...T) error {
	rbac.mutex.Lock()
	defer rbac.mutex.Unlock()
	if _, ok := rbac.roles[id]; !ok {
		return ErrRoleNotExist
	}
	for _, parent := range parents {
		if _, ok := rbac.roles[parent]; !ok {
			return ErrRoleNotExist
		}
	}
	for _, parent := range parents {
		delete(rbac.parents[id], parent)
	}
	return nil
}

// Add a role `r`.
func (rbac *StdRBAC[T]) Add(ctx context.Context, r Role[T]) (err error) {
	rbac.mutex.Lock()
	id := r.ID()
	if _, ok := rbac.roles[id]; !ok {
		rbac.roles[id] = r
	} else {
		err = ErrRoleExist
	}
	rbac.mutex.Unlock()
	return
}

// Remove the role by `id`.
func (rbac *StdRBAC[T]) Remove(_ context.Context, id T) (err error) {
	rbac.mutex.Lock()
	if _, ok := rbac.roles[id]; ok {
		delete(rbac.roles, id)
		for rid, parents := range rbac.parents {
			if rid == id {
				delete(rbac.parents, rid)
				continue
			}
			for parent := range parents {
				if parent == id {
					delete(rbac.parents[rid], id)
					break
				}
			}
		}
	} else {
		err = ErrRoleNotExist
	}
	rbac.mutex.Unlock()
	return
}

// Get returns the role by `id`.
func (rbac *StdRBAC[T]) Get(_ context.Context, id T) (r Role[T], err error) {
	rbac.mutex.RLock()
	var ok bool
	if r, ok = rbac.roles[id]; !ok {
		err = ErrRoleNotExist
	}
	rbac.mutex.RUnlock()
	return
}

// RoleIDs returns all role IDs.
func (rbac *StdRBAC[T]) RoleIDs(_ context.Context) []T {
	rbac.mutex.RLock()
	ids := make([]T, 0, len(rbac.roles))
	for id := range rbac.roles {
		ids = append(ids, id)
	}
	rbac.mutex.RUnlock()
	return ids
}

// IsGranted tests if the role `id` has permission `p`.
func (rbac *StdRBAC[T]) IsGranted(ctx context.Context, id T, p Permission[T]) (ok bool) {
	rbac.mutex.RLock()
	ok = rbac.isGranted(ctx, id, p)
	rbac.mutex.RUnlock()
	return
}

func (rbac *StdRBAC[T]) isGranted(ctx context.Context, id T, p Permission[T]) bool {
	return rbac.recursionCheck(ctx, id, p)
}

func (rbac *StdRBAC[T]) recursionCheck(ctx context.Context, id T, p Permission[T]) bool {
	if role, ok := rbac.roles[id]; ok {
		if role.Permit(ctx, p) {
			return true
		}
		if parents, ok := rbac.parents[id]; ok {
			for pID := range parents {
				if _, ok := rbac.roles[pID]; ok {
					if rbac.recursionCheck(ctx, pID, p) {
						return true
					}
				}
			}
		}
	}
	return false
}
