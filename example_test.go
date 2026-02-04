package gorbac_test

import (
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

	must(rA.Assign(pA))
	must(rB.Assign(pB))
	must(rC.Assign(pC))
	must(rD.Assign(pD))
	must(rE.Assign(pE))

	must(rbac.Add(rA))
	must(rbac.Add(rB))
	must(rbac.Add(rC))
	must(rbac.Add(rD))
	must(rbac.Add(rE))
	must(rbac.SetParent("role-a", "role-b"))
	must(rbac.SetParents("role-b", []string{"role-c", "role-d"}))
	must(rbac.SetParent("role-e", "role-d"))

	if rbac.IsGranted("role-a", pA, nil) &&
		rbac.IsGranted("role-a", pB, nil) &&
		rbac.IsGranted("role-a", pC, nil) &&
		rbac.IsGranted("role-a", pD, nil) {
		fmt.Println("The role-a has been granted permis-a, b, c and d.")
	}
	if rbac.IsGranted("role-b", pB, nil) &&
		rbac.IsGranted("role-b", pC, nil) &&
		rbac.IsGranted("role-b", pD, nil) {
		fmt.Println("The role-b has been granted permis-b, c and d.")
	}
	// When a circle inheratance occurred,
	must(rbac.SetParent("role-c", "role-a"))
	// it could be detected as following code:
	if err := gorbac.InherCircle(rbac); err != nil {
		fmt.Println("A circle inheratance occurred.")
	}
	// Output:
	// The role-a has been granted permis-a, b, c and d.
	// The role-b has been granted permis-b, c and d.
	// A circle inheratance occurred.
}

func ExampleRBAC_int() {
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

	must(rA.Assign(pA))
	must(rB.Assign(pB))
	must(rC.Assign(pC))
	must(rD.Assign(pD))
	must(rE.Assign(pE))

	must(rbac.Add(rA))
	must(rbac.Add(rB))
	must(rbac.Add(rC))
	must(rbac.Add(rD))
	must(rbac.Add(rE))
	must(rbac.SetParent(1, 2))
	must(rbac.SetParents(2, []int{3, 4}))
	must(rbac.SetParent(5, 4))

	if rbac.IsGranted(1, pA, nil) &&
		rbac.IsGranted(1, pB, nil) &&
		rbac.IsGranted(1, pC, nil) &&
		rbac.IsGranted(1, pD, nil) {
		fmt.Println("The role-a has been granted permis-a, b, c and d.")
	}
	if rbac.IsGranted(2, pB, nil) &&
		rbac.IsGranted(2, pC, nil) &&
		rbac.IsGranted(2, pD, nil) {
		fmt.Println("The role-b has been granted permis-b, c and d.")
	}
	// When a circle inheratance occurred,
	must(rbac.SetParent(3, 1))
	// it could be detected as following code:
	if err := gorbac.InherCircle(rbac); err != nil {
		fmt.Println("A circle inheratance occurred.")
	}
	// Output:
	// The role-a has been granted permis-a, b, c and d.
	// The role-b has been granted permis-b, c and d.
	// A circle inheratance occurred.
}
