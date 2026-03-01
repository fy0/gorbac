package main

import (
	"context"
	"fmt"
	"log"

	"github.com/fy0/gorbac/v3"
)

type roleRepo[T comparable] interface {
	AddRole(id T) error
	RemoveRole(id T) error
	AddParent(id, parent T) error
	RemoveParents(id T, parents ...T) error
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

func (r *memoryRepo[T]) RemoveParents(id T, parents ...T) error {
	if _, ok := r.parents[id]; ok {
		for _, parent := range parents {
			delete(r.parents[id], parent)
		}
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

func (r *MyRBAC[T]) Add(ctx context.Context, role gorbac.Role[T]) error {
	if err := r.repo.AddRole(role.ID()); err != nil {
		return err
	}
	return r.inner.Add(ctx, role)
}

func (r *MyRBAC[T]) Remove(ctx context.Context, id T) error {
	if err := r.repo.RemoveRole(id); err != nil {
		return err
	}
	return r.inner.Remove(ctx, id)
}

func (r *MyRBAC[T]) SetParents(ctx context.Context, id T, parents ...T) error {
	for _, parent := range parents {
		if err := r.repo.AddParent(id, parent); err != nil {
			return err
		}
	}
	return r.inner.SetParents(ctx, id, parents...)
}

func (r *MyRBAC[T]) GetParents(ctx context.Context, id T) ([]T, error) {
	return r.inner.GetParents(ctx, id)
}

func (r *MyRBAC[T]) RemoveParents(ctx context.Context, id T, parents ...T) error {
	if err := r.repo.RemoveParents(id, parents...); err != nil {
		return err
	}
	return r.inner.RemoveParents(ctx, id, parents...)
}

func (r *MyRBAC[T]) Get(ctx context.Context, id T) (gorbac.Role[T], error) {
	return r.inner.Get(ctx, id)
}

func (r *MyRBAC[T]) RoleIDs(ctx context.Context) []T {
	return r.inner.RoleIDs(ctx)
}

func (r *MyRBAC[T]) IsGranted(ctx context.Context, id T, p gorbac.Permission[T]) bool {
	return r.inner.IsGranted(ctx, id, p)
}

func main() {
	ctx := context.Background()
	repo := newMemoryRepo[string]()
	rbac := NewMyRBAC[string](repo)

	admin := gorbac.NewRole("admin")
	user := gorbac.NewRole("user")
	read := gorbac.NewPermission("read")

	if err := admin.Assign(ctx, read); err != nil {
		log.Fatal(err)
	}
	if err := user.Assign(ctx, read); err != nil {
		log.Fatal(err)
	}

	if err := rbac.Add(ctx, admin); err != nil {
		log.Fatal(err)
	}
	if err := rbac.Add(ctx, user); err != nil {
		log.Fatal(err)
	}
	if err := rbac.SetParents(ctx, "admin", "user"); err != nil {
		log.Fatal(err)
	}

	if rbac.IsGranted(ctx, "admin", read) {
		fmt.Println("admin can read")
	}

	fmt.Printf("repo roles: %d\n", len(repo.roles))
	fmt.Printf("repo parents: %d\n", len(repo.parents["admin"]))
}
