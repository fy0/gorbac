package main

import (
	"fmt"
	"log"

	"github.com/fy0/gorbac/v3"
)

type roleRepo[T comparable] interface {
	AddRole(id T) error
	RemoveRole(id T) error
	AddParent(id, parent T) error
	RemoveParent(id, parent T) error
}

type memoryRepo[T comparable] struct {
	roles   map[T]struct{}
	parents map[T]map[T]struct{}
}

func newMemoryRepo[T comparable]() *memoryRepo[T] {
	return &memoryRepo[T]{
		roles:   make(map[T]struct{}),
		parents: make(map[T]map[T]struct{}),
	}
}

func (r *memoryRepo[T]) AddRole(id T) error {
	r.roles[id] = struct{}{}
	return nil
}

func (r *memoryRepo[T]) RemoveRole(id T) error {
	delete(r.roles, id)
	delete(r.parents, id)
	for child, parents := range r.parents {
		if _, ok := parents[id]; ok {
			delete(parents, id)
			r.parents[child] = parents
		}
	}
	return nil
}

func (r *memoryRepo[T]) AddParent(id, parent T) error {
	if _, ok := r.parents[id]; !ok {
		r.parents[id] = make(map[T]struct{})
	}
	r.parents[id][parent] = struct{}{}
	return nil
}

func (r *memoryRepo[T]) RemoveParent(id, parent T) error {
	if _, ok := r.parents[id]; ok {
		delete(r.parents[id], parent)
	}
	return nil
}

type MyRBAC[T comparable] struct {
	inner *gorbac.StdRBAC[T]
	repo  roleRepo[T]
}

func NewMyRBAC[T comparable](repo roleRepo[T]) *MyRBAC[T] {
	return &MyRBAC[T]{
		inner: gorbac.New[T](),
		repo:  repo,
	}
}

func (r *MyRBAC[T]) Add(role gorbac.Role[T]) error {
	if err := r.repo.AddRole(role.ID()); err != nil {
		return err
	}
	return r.inner.Add(role)
}

func (r *MyRBAC[T]) Remove(id T) error {
	if err := r.repo.RemoveRole(id); err != nil {
		return err
	}
	return r.inner.Remove(id)
}

func (r *MyRBAC[T]) SetParent(id T, parent T) error {
	if err := r.repo.AddParent(id, parent); err != nil {
		return err
	}
	return r.inner.SetParent(id, parent)
}

func (r *MyRBAC[T]) SetParents(id T, parents []T) error {
	for _, parent := range parents {
		if err := r.repo.AddParent(id, parent); err != nil {
			return err
		}
	}
	return r.inner.SetParents(id, parents)
}

func (r *MyRBAC[T]) GetParents(id T) ([]T, error) {
	return r.inner.GetParents(id)
}

func (r *MyRBAC[T]) RemoveParent(id T, parent T) error {
	if err := r.repo.RemoveParent(id, parent); err != nil {
		return err
	}
	return r.inner.RemoveParent(id, parent)
}

func (r *MyRBAC[T]) Get(id T) (gorbac.Role[T], []T, error) {
	return r.inner.Get(id)
}

func (r *MyRBAC[T]) RoleIDs() []T {
	return r.inner.RoleIDs()
}

func (r *MyRBAC[T]) IsGranted(id T, p gorbac.Permission[T], assert gorbac.AssertionFunc[T]) bool {
	return r.inner.IsGranted(id, p, assert)
}

func main() {
	repo := newMemoryRepo[string]()
	rbac := NewMyRBAC[string](repo)

	admin := gorbac.NewRole("admin")
	user := gorbac.NewRole("user")
	read := gorbac.NewPermission("read")

	if err := admin.Assign(read); err != nil {
		log.Fatal(err)
	}
	if err := user.Assign(read); err != nil {
		log.Fatal(err)
	}

	if err := rbac.Add(admin); err != nil {
		log.Fatal(err)
	}
	if err := rbac.Add(user); err != nil {
		log.Fatal(err)
	}
	if err := rbac.SetParent("admin", "user"); err != nil {
		log.Fatal(err)
	}

	if rbac.IsGranted("admin", read, nil) {
		fmt.Println("admin can read")
	}

	fmt.Printf("repo roles: %d\n", len(repo.roles))
	fmt.Printf("repo parents: %d\n", len(repo.parents["admin"]))
}
