package gorbac

import (
	"context"
	"testing"
)

var (
	rA = NewRole("role-a")
	pA = NewPermission("permission-a")
	rB = NewRole("role-b")
	pB = NewPermission("permission-b")
	rC = NewRole("role-c")
	pC = NewPermission("permission-c")

	rbac *StdRBAC[string]

	permissionZero Permission[string]
)

func assert(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestRbacPrepare(t *testing.T) {
	ctx := context.Background()
	rbac = New[string]()
	assert(t, rA.Assign(ctx, pA))
	assert(t, rB.Assign(ctx, pB))
	assert(t, rC.Assign(ctx, pC))
}

func TestRbacAdd(t *testing.T) {
	ctx := context.Background()
	assert(t, rbac.Add(ctx, rA))
	if err := rbac.Add(ctx, rA); err != ErrRoleExist {
		t.Error("A role can not be readded")
	}
	assert(t, rbac.Add(ctx, rB))
	assert(t, rbac.Add(ctx, rC))
}

func TestRbacGetRemove(t *testing.T) {
	ctx := context.Background()
	assert(t, rbac.SetParents(ctx, "role-c", "role-a"))
	assert(t, rbac.SetParents(ctx, "role-a", "role-b"))
	if r, err := rbac.Get(ctx, "role-a"); err != nil {
		t.Fatal(err)
	} else if r.ID() != "role-a" {
		t.Fatalf("[role-a] does not match %s", r.ID())
	}
	if parents, err := rbac.GetParents(ctx, "role-a"); err != nil {
		t.Fatal(err)
	} else if len(parents) != 1 {
		t.Fatal("[role-a] should have one parent")
	}
	assert(t, rbac.Remove(ctx, "role-a"))
	if err := rbac.Remove(ctx, "not-exist"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if r, err := rbac.Get(ctx, "role-a"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	} else if r != nil {
		t.Fatal("The role should be nil when not found")
	}
}

func TestRbacParents(t *testing.T) {
	ctx := context.Background()
	rD := NewRole("role-d")
	assert(t, rbac.Add(ctx, rD))
	assert(t, rbac.SetParents(ctx, "role-c", "role-b", "role-d"))
	if parents, err := rbac.GetParents(ctx, "role-c"); err != nil {
		t.Fatal(err)
	} else if !containsParent(parents, "role-b") {
		t.Fatal("Parent binding failed")
	} else if !containsParent(parents, "role-d") {
		t.Fatal("Parent binding failed")
	}
	assert(t, rbac.RemoveParents(ctx, "role-c", "role-b", "role-d"))
	if parents, err := rbac.GetParents(ctx, "role-c"); err != nil {
		t.Fatal(err)
	} else if containsParent(parents, "role-b") || containsParent(parents, "role-d") {
		t.Fatal("Parent unbinding failed")
	}
	if err := rbac.RemoveParents(ctx, "role-a", "role-b"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if err := rbac.RemoveParents(ctx, "role-b", "role-a"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if err := rbac.SetParents(ctx, "role-a", "role-b"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if err := rbac.SetParents(ctx, "role-c", "role-a"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if err := rbac.SetParents(ctx, "role-a", "role-b"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	if err := rbac.SetParents(ctx, "role-c", "role-a"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	}
	assert(t, rbac.SetParents(ctx, "role-c", "role-b"))
	if parents, err := rbac.GetParents(ctx, "role-c"); err != nil {
		t.Fatal(err)
	} else if !containsParent(parents, "role-b") {
		t.Fatal("Parent binding failed")
	}
	if parents, err := rbac.GetParents(ctx, "role-a"); err != ErrRoleNotExist {
		t.Fatalf("%s needed", ErrRoleNotExist)
	} else if len(parents) != 0 {
		t.Fatal("[role-a] should not have any parent")
	}
	if parents, err := rbac.GetParents(ctx, "role-b"); err != nil {
		t.Fatal(err)
	} else if len(parents) != 0 {
		t.Fatal("[role-b] should not have any parent")
	}
	if parents, err := rbac.GetParents(ctx, "role-c"); err != nil {
		t.Fatal(err)
	} else if len(parents) != 1 {
		t.Fatal("[role-c] should have one parent")
	}
}

func TestRbacPermission(t *testing.T) {
	ctx := context.Background()
	if !rbac.IsGranted(ctx, "role-c", pC) {
		t.Fatalf("role-c should have %s", pC)
	}
	if !rbac.IsGranted(ctx, "role-c", pB) {
		t.Fatalf("role-c should have %s which inherits from role-b", pB)
	}

	assert(t, rbac.RemoveParents(ctx, "role-c", "role-b"))
	if rbac.IsGranted(ctx, "role-c", pB) {
		t.Fatalf("role-c should not have %s because of the unbinding with role-b", pB)
	}

	if rbac.IsGranted(ctx, "role-a", permissionZero) {
		t.Fatal("role-a should not have nil permission")
	}
}

func containsParent(parents []string, target string) bool {
	for _, parent := range parents {
		if parent == target {
			return true
		}
	}
	return false
}

func BenchmarkRbacGranted(b *testing.B) {
	ctx := context.Background()
	rbac = New[string]()
	if err := rA.Assign(ctx, pA); err != nil {
		b.Fatal(err)
	}
	if err := rB.Assign(ctx, pB); err != nil {
		b.Fatal(err)
	}
	if err := rC.Assign(ctx, pC); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rA); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rB); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rC); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		rbac.IsGranted(ctx, "role-a", pA)
	}
}

func BenchmarkRbacNotGranted(b *testing.B) {
	ctx := context.Background()
	rbac = New[string]()
	if err := rA.Assign(ctx, pA); err != nil {
		b.Fatal(err)
	}
	if err := rB.Assign(ctx, pB); err != nil {
		b.Fatal(err)
	}
	if err := rC.Assign(ctx, pC); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rA); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rB); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rC); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		rbac.IsGranted(ctx, "role-a", pB)
	}
}
