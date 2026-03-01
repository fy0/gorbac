package gorbac_test

import (
	"context"
	"fmt"

	"github.com/fy0/gorbac/v3"
)

/*
Suppose:

The role-a is inheriting from role-b.
The role-b is inheriting from role-c, role-d.
The role-c is individual.
The role-d is individual.
The role-e is inheriting from role-d.
Every roles have their own permissions.
*/
func ExampleRBAC_string() {
	ctx := context.Background()
	rbac := gorbac.New[string]()
	rA := gorbac.NewRole("role-a")
	rB := gorbac.NewRole("role-b")
	rC := gorbac.NewRole("role-c")
	rD := gorbac.NewRole("role-d")
	rE := gorbac.NewRole("role-e")

	pA := gorbac.NewPermission("permission-a")
	pB := gorbac.NewPermission("permission-b")
	pC := gorbac.NewPermission("permission-c")
	pD := gorbac.NewPermission("permission-d")
	pE := gorbac.NewPermission("permission-e")

	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}

	must(rA.Assign(ctx, pA))
	must(rB.Assign(ctx, pB))
	must(rC.Assign(ctx, pC))
	must(rD.Assign(ctx, pD))
	must(rE.Assign(ctx, pE))

	must(rbac.Add(ctx, rA))
	must(rbac.Add(ctx, rB))
	must(rbac.Add(ctx, rC))
	must(rbac.Add(ctx, rD))
	must(rbac.Add(ctx, rE))
	must(rbac.SetParents(ctx, "role-a", "role-b"))
	must(rbac.SetParents(ctx, "role-b", "role-c", "role-d"))
	must(rbac.SetParents(ctx, "role-e", "role-d"))

	if rbac.IsGranted(ctx, "role-a", pA) &&
		rbac.IsGranted(ctx, "role-a", pB) &&
		rbac.IsGranted(ctx, "role-a", pC) &&
		rbac.IsGranted(ctx, "role-a", pD) {
		fmt.Println("The role-a has been granted permis-a, b, c and d.")
	}
	if rbac.IsGranted(ctx, "role-b", pB) &&
		rbac.IsGranted(ctx, "role-b", pC) &&
		rbac.IsGranted(ctx, "role-b", pD) {
		fmt.Println("The role-b has been granted permis-b, c and d.")
	}
	// When a circle inheratance occurred,
	must(rbac.SetParents(ctx, "role-c", "role-a"))
	// it could be detected as following code:
	if err := gorbac.InherCircle(ctx, rbac); err != nil {
		fmt.Println("A circle inheratance occurred.")
	}
	// Output:
	// The role-a has been granted permis-a, b, c and d.
	// The role-b has been granted permis-b, c and d.
	// A circle inheratance occurred.
}

func ExampleRBAC_int() {
	ctx := context.Background()
	rbac := gorbac.New[int]()
	rA := gorbac.NewRole(1)
	rB := gorbac.NewRole(2)
	rC := gorbac.NewRole(3)
	rD := gorbac.NewRole(4)
	rE := gorbac.NewRole(5)

	pA := gorbac.NewPermission(1)
	pB := gorbac.NewPermission(2)
	pC := gorbac.NewPermission(3)
	pD := gorbac.NewPermission(4)
	pE := gorbac.NewPermission(5)

	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}

	must(rA.Assign(ctx, pA))
	must(rB.Assign(ctx, pB))
	must(rC.Assign(ctx, pC))
	must(rD.Assign(ctx, pD))
	must(rE.Assign(ctx, pE))

	must(rbac.Add(ctx, rA))
	must(rbac.Add(ctx, rB))
	must(rbac.Add(ctx, rC))
	must(rbac.Add(ctx, rD))
	must(rbac.Add(ctx, rE))
	must(rbac.SetParents(ctx, 1, 2))
	must(rbac.SetParents(ctx, 2, 3, 4))
	must(rbac.SetParents(ctx, 5, 4))

	if rbac.IsGranted(ctx, 1, pA) &&
		rbac.IsGranted(ctx, 1, pB) &&
		rbac.IsGranted(ctx, 1, pC) &&
		rbac.IsGranted(ctx, 1, pD) {
		fmt.Println("The role-a has been granted permis-a, b, c and d.")
	}
	if rbac.IsGranted(ctx, 2, pB) &&
		rbac.IsGranted(ctx, 2, pC) &&
		rbac.IsGranted(ctx, 2, pD) {
		fmt.Println("The role-b has been granted permis-b, c and d.")
	}
	// When a circle inheratance occurred,
	must(rbac.SetParents(ctx, 3, 1))
	// it could be detected as following code:
	if err := gorbac.InherCircle(ctx, rbac); err != nil {
		fmt.Println("A circle inheratance occurred.")
	}
	// Output:
	// The role-a has been granted permis-a, b, c and d.
	// The role-b has been granted permis-b, c and d.
	// A circle inheratance occurred.
}
