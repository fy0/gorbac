package gorbac

import (
	"context"
	"testing"
)

func TestStdrA(t *testing.T) {
	ctx := context.Background()
	rA := NewRole("role-a")
	if rA.ID() != "role-a" {
		t.Fatalf("[a] expected, but %s got", rA.ID())
	}
	if err := rA.Assign(ctx, NewPermission("permission-a")); err != nil {
		t.Fatal(err)
	}
	if !rA.Permit(ctx, NewPermission("permission-a")) {
		t.Fatal("[permission-a] should permit to rA")
	}
	if len(rA.Permissions(ctx)) != 1 {
		t.Fatal("[a] should have one permission")
	}

	if err := rA.Revoke(ctx, NewPermission("permission-a")); err != nil {
		t.Fatal(err)
	}
	if rA.Permit(ctx, NewPermission("permission-a")) {
		t.Fatal("[permission-a] should not permit to rA")
	}
	if len(rA.Permissions(ctx)) != 0 {
		t.Fatal("[a] should not have any permission")
	}
}
