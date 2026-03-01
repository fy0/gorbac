package gorbac

import (
	"context"
	"errors"
	"testing"
)

var (
	pAll  = NewPermission("permission-all")
	pNone = NewPermission("permission-none")
)

func TestPrepareCircle(t *testing.T) {
	ctx := context.Background()
	rbac = New[string]()
	assert(t, rA.Assign(ctx, pA))
	assert(t, rB.Assign(ctx, pB))
	assert(t, rC.Assign(ctx, pC))
	assert(t, rA.Assign(ctx, pAll))
	assert(t, rB.Assign(ctx, pAll))
	assert(t, rC.Assign(ctx, pAll))
	assert(t, rbac.Add(ctx, rA))
	assert(t, rbac.Add(ctx, rB))
	assert(t, rbac.Add(ctx, rC))
	assert(t, rbac.SetParent(ctx, "role-a", "role-b"))
	assert(t, rbac.SetParent(ctx, "role-b", "role-c"))
	assert(t, rbac.SetParent(ctx, "role-c", "role-a"))
}

func TestInherCircle(t *testing.T) {
	ctx := context.Background()
	if err := InherCircle(ctx, rbac); err == nil {
		t.Fatal("There should be a circle inheritance.")
	} else {
		t.Log(err)
	}
}

func TestInherNormal(t *testing.T) {
	ctx := context.Background()
	assert(t, rbac.RemoveParent(ctx, "role-c", "role-a"))
	if err := InherCircle(ctx, rbac); err != nil {
		t.Fatal(err)
	}
}

func TestAllGranted(t *testing.T) {
	ctx := context.Background()
	// All roles have pAll
	roles := []string{"role-a", "role-b", "role-c"}
	if !AllGranted(ctx, rbac, roles, pAll, nil) {
		t.Errorf("All roles(%v) were expected having %s, but they weren't.", roles, pAll)
	}

	if AllGranted(ctx, rbac, roles, pA, nil) {
		t.Errorf("Not all roles(%v) were expected having %s, but they were.", roles, pA)
	}
}

func TestAnyGranted(t *testing.T) {
	ctx := context.Background()
	// rA roles have pA
	roles := []string{"role-a", "role-b", "role-c"}
	if !AnyGranted(ctx, rbac, roles, pA, nil) {
		t.Errorf("One of roles(%v) was expected having %s, but it wasn't.", roles, pA)
	}

	if AnyGranted(ctx, rbac, roles, pNone, nil) {
		t.Errorf("None of roles(%v) were expected having %s, but it was.", roles, pNone)
	}

}

func TestWalk(t *testing.T) {
	ctx := context.Background()
	if err := Walk(ctx, rbac, nil); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	h := func(r Role[string], parents []string) error {
		t.Logf("Role: %v", r.ID())
		permissions := make([]string, 0)
		for _, p := range r.Permissions(ctx) {
			permissions = append(permissions, p.ID())
		}
		t.Logf("Permission: %v", permissions)
		t.Logf("Parents: %v", parents)
		return nil
	}
	if err := Walk(ctx, rbac, h); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	he := func(r Role[string], parents []string) error {
		return errors.New("Expected error")
	}
	if err := Walk(ctx, rbac, he); err == nil {
		t.Errorf("Expected error, got nil")
	}
}

func BenchmarkInherCircle(b *testing.B) {
	ctx := context.Background()
	rbac = New[string]()
	if err := rbac.Add(ctx, rA); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rB); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rC); err != nil {
		b.Fatal(err)
	}
	if err := rbac.SetParent(ctx, "role-a", "role-b"); err != nil {
		b.Fatal(err)
	}
	if err := rbac.SetParent(ctx, "role-b", "role-c"); err != nil {
		b.Fatal(err)
	}
	if err := rbac.SetParent(ctx, "role-c", "role-a"); err != nil {
		b.Fatal(err)
	}
	if err := InherCircle(ctx, rbac); err == nil {
		b.Fatal("expected circle inheritance error")
	}
	for i := 0; i < b.N; i++ {
		_ = InherCircle(ctx, rbac)
	}
}

func BenchmarkInherNormal(b *testing.B) {
	ctx := context.Background()
	rbac = New[string]()
	if err := rbac.Add(ctx, rA); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rB); err != nil {
		b.Fatal(err)
	}
	if err := rbac.Add(ctx, rC); err != nil {
		b.Fatal(err)
	}
	if err := rbac.SetParent(ctx, "role-a", "role-b"); err != nil {
		b.Fatal(err)
	}
	if err := rbac.SetParent(ctx, "role-b", "role-c"); err != nil {
		b.Fatal(err)
	}
	if err := InherCircle(ctx, rbac); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_ = InherCircle(ctx, rbac)
	}
}
