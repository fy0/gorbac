package gorbac

import (
	"sync"
)

// Roles is a map
type Roles[T comparable] map[T]Role[T]

// NewStdRole is the default role factory function.
// It matches the declaration to RoleFactoryFunc.
func NewRole[T comparable](id T) Role[T] {
	return Role[T]{
		mutex:       new(sync.RWMutex),
		ID:          id,
		permissions: make(Permissions[T]),
	}
}

// StdRole is the default role implement.
// You can combine this struct into your own Role implement.
// T is the type of ID
type Role[T comparable] struct {
	mutex *sync.RWMutex
	// ID is the serialisable identity of role
	ID          T `json:"id"`
	permissions Permissions[T]
}

func (role *Role[T]) init() {
	if role.mutex == nil {
		role.mutex = new(sync.RWMutex)
	}
	if role.permissions == nil {
		role.permissions = make(Permissions[T])
	}
}

// Assign a permission to the role.
func (role *Role[T]) Assign(p Permission[T]) error {
	role.init()
	role.mutex.Lock()
	role.permissions[p.ID()] = p
	role.mutex.Unlock()
	return nil
}

// Permit returns true if the role has specific permission.
func (role *Role[T]) Permit(p Permission[T]) (ok bool) {
	var zero Permission[T]
	if p == zero {
		return false
	}

	role.init()
	role.mutex.RLock()
	// Fast path: permission IDs are used as map keys for exact matches.
	//
	// This preserves existing behavior for layered / custom matching because
	// we still fall back to scanning the full permission set when needed.
	if rp, exists := role.permissions[p.ID()]; exists {
		if rp.Match(p) {
			role.mutex.RUnlock()
			return true
		}
	}
	for _, rp := range role.permissions {
		if rp.Match(p) {
			ok = true
			break
		}
	}
	role.mutex.RUnlock()
	return
}

// Revoke the specific permission.
func (role *Role[T]) Revoke(p Permission[T]) error {
	role.init()
	role.mutex.Lock()
	delete(role.permissions, p.ID())
	role.mutex.Unlock()
	return nil
}

// Permissions returns all permissions into a slice.
func (role *Role[T]) Permissions() []Permission[T] {
	role.init()
	role.mutex.RLock()
	result := make([]Permission[T], 0, len(role.permissions))
	for _, p := range role.permissions {
		result = append(result, p)
	}
	role.mutex.RUnlock()
	return result
}
