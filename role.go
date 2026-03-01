package gorbac

import (
	"context"
	"sync"
)

// Role describes the role contract.
type Role[T comparable] interface {
	ID() T
	Assign(context.Context, ...Permission[T]) error
	Permit(context.Context, ...Permission[T]) bool
	Revoke(context.Context, ...Permission[T]) error
	Permissions(context.Context) []Permission[T]
	PermissionsMap(context.Context) map[T]Permission[T]
	Get(context.Context, T) (Permission[T], bool)
	FilterPermissions(context.Context) map[T]Permission[T]
}

// Roles is a map
type Roles[T comparable] map[T]Role[T]

// NewRole is the default role factory function.
func NewRole[T comparable](id T) *StdRole[T] {
	return &StdRole[T]{
		mutex:             new(sync.RWMutex),
		IDValue:           id,
		permissions:       make(Permissions[T]),
		filterPermissions: make(map[T]Permission[T]),
	}
}

// StdRole is the default role implementation.
// You can embed this struct into your own role implementation.
// T is the type of ID.
type StdRole[T comparable] struct {
	mutex *sync.RWMutex
	// ID is the serialisable identity of role
	IDValue           T `json:"id"`
	permissions       Permissions[T]
	filterPermissions map[T]Permission[T]
}

func (role *StdRole[T]) init() {
	if role.mutex == nil {
		role.mutex = new(sync.RWMutex)
	}
	if role.permissions == nil {
		role.permissions = make(Permissions[T])
	}
	if role.filterPermissions == nil {
		role.filterPermissions = make(map[T]Permission[T])
	}
}

// ID returns the role ID.
func (role *StdRole[T]) ID() T {
	return role.IDValue
}

// Assign permissions to the role.
func (role *StdRole[T]) Assign(_ context.Context, perms ...Permission[T]) error {
	if len(perms) == 0 {
		return nil
	}
	role.init()
	role.mutex.Lock()
	for _, p := range perms {
		role.permissions[p.ID()] = p
		if _, ok := p.(interface {
			CEL() (string, error)
		}); ok {
			role.filterPermissions[p.ID()] = p
		} else {
			delete(role.filterPermissions, p.ID())
		}
	}
	role.mutex.Unlock()
	return nil
}

// Permit returns true if the role has all specified permissions.
func (role *StdRole[T]) Permit(_ context.Context, perms ...Permission[T]) bool {
	if len(perms) == 0 {
		return false
	}
	var zero Permission[T]
	role.init()
	role.mutex.RLock()
	for _, p := range perms {
		if p == zero {
			role.mutex.RUnlock()
			return false
		}
		matched := false
		// Fast path: permission IDs are used as map keys for exact matches.
		//
		// This preserves existing behavior for layered / custom matching because
		// we still fall back to scanning the full permission set when needed.
		if rp, exists := role.permissions[p.ID()]; exists && rp.Match(p) {
			matched = true
		} else {
			for _, rp := range role.permissions {
				if rp.Match(p) {
					matched = true
					break
				}
			}
		}
		if !matched {
			role.mutex.RUnlock()
			return false
		}
	}
	role.mutex.RUnlock()
	return true
}

// Revoke the specific permissions.
func (role *StdRole[T]) Revoke(_ context.Context, perms ...Permission[T]) error {
	if len(perms) == 0 {
		return nil
	}
	role.init()
	role.mutex.Lock()
	for _, p := range perms {
		delete(role.permissions, p.ID())
		delete(role.filterPermissions, p.ID())
	}
	role.mutex.Unlock()
	return nil
}

// Permissions returns all permissions into a slice.
func (role *StdRole[T]) Permissions(_ context.Context) []Permission[T] {
	role.init()
	role.mutex.RLock()
	result := make([]Permission[T], 0, len(role.permissions))
	for _, p := range role.permissions {
		result = append(result, p)
	}
	role.mutex.RUnlock()
	return result
}

// PermissionsMap returns a raw ref of permissions keyed by ID.
func (role *StdRole[T]) PermissionsMap(_ context.Context) map[T]Permission[T] {
	role.init()
	return role.permissions
}

// Get returns a permission by ID.
func (role *StdRole[T]) Get(_ context.Context, id T) (Permission[T], bool) {
	role.init()
	role.mutex.RLock()
	p, ok := role.permissions[id]
	role.mutex.RUnlock()
	return p, ok
}

// FilterPermissions returns a raw ref of CEL-carrying permissions keyed by ID.
func (role *StdRole[T]) FilterPermissions(_ context.Context) map[T]Permission[T] {
	role.init()
	return role.filterPermissions
}
