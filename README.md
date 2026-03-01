goRBAC 
======

[![Build Status](https://travis-ci.org/fy0/gorbac.png?branch=master)](https://travis-ci.org/fy0/gorbac)
[![Go Reference](https://pkg.go.dev/badge/github.com/fy0/gorbac/v3.svg)](https://pkg.go.dev/github.com/fy0/gorbac/v3)
[![Coverage Status](https://coveralls.io/repos/github/fy0/gorbac/badge.svg?branch=master)](https://coveralls.io/github/fy0/gorbac?branch=master)

goRBAC provides a lightweight role-based access control implementation
in Golang.

For the purposes of this package:

	* an identity has one or more roles.
	* a role requests access to a permission.
	* a permission is given to a role.

Thus, RBAC has the following model:

	* many to many relationship between identities and roles.
	* many to many relationship between roles and permissions.
	* roles can have parent roles (inheriting permissions).

Version
=======

The current version of goRBAC uses Go generics (Go 1.18+) and is under active development.

About this fork
===============

This fork includes a few behavior/API adjustments:

- `Role` is now an interface and the default implementation is `StdRole` (constructed via `NewRole`).
- `RBAC` is now an interface and the default implementation is `StdRBAC` (constructed via `New`).
- RBAC/Role APIs use `context.Context` as the first parameter, aligned with modern Go practices.
- `Role.Assign`, `Role.Permit`, and `Role.Revoke` now accept variadic permissions for batch usage.
- `RBAC.Get` now returns only `Role`; use `RBAC.GetParents` when parent IDs are needed.
- `RBAC.SetParent` has been removed; use `RBAC.SetParents(ctx, id, parentID)` instead.
- `RBAC.SetParents` now accepts variadic parent IDs (`parents ...T`).
- `RBAC.RemoveParents` now accepts variadic parent IDs (`parents ...T`).
- `AnyGranted` and `AllGranted` now accept variadic permissions for batch checks.
- The data-scope filter helpers focus on composing CEL filters across roles; permission checks are expected to happen elsewhere.

Install
=======

Install the package:

```bash
$ go get github.com/fy0/gorbac/v3
```

Usage
=====

Although you can adjust the RBAC instance anytime and it's absolutely safe, the library is designed for use with two phases:

1. Preparing

2. Checking

Preparing
---------

Import the library:

```go
import (
	"context"

	"github.com/fy0/gorbac/v3"
)
```

Get a new instance of RBAC (using string as the ID type):

```go
ctx := context.Background()
rbac := gorbac.New[string]()
```

Get some new roles:

```go
rA := gorbac.NewRole("role-a")
rB := gorbac.NewRole("role-b")
rC := gorbac.NewRole("role-c")
rD := gorbac.NewRole("role-d")
rE := gorbac.NewRole("role-e")
```

Get some new permissions:

```go
pA := gorbac.NewPermission("permission-a")
pB := gorbac.NewPermission("permission-b")
pC := gorbac.NewPermission("permission-c")
pD := gorbac.NewPermission("permission-d")
pE := gorbac.NewPermission("permission-e")
```

Add the permissions to roles:

```go
rA.Assign(ctx, pA)
rB.Assign(ctx, pB)
rC.Assign(ctx, pC)
rD.Assign(ctx, pD)
rE.Assign(ctx, pE)
```

Also, you can implement `gorbac.Permission` for your own data structure.

After initialization, add the roles to the RBAC instance:

```go
rbac.Add(ctx, rA)
rbac.Add(ctx, rB)
rbac.Add(ctx, rC)
rbac.Add(ctx, rD)
rbac.Add(ctx, rE)
```

And set the inheritance:

```go
rbac.SetParents(ctx, "role-a", "role-b")
rbac.SetParents(ctx, "role-b", "role-c", "role-d")
rbac.SetParents(ctx, "role-e", "role-d")
```

Checking
--------

Checking the permission is easy:

```go
if rbac.IsGranted(ctx, "role-a", pA) &&
	rbac.IsGranted(ctx, "role-a", pB) &&
	rbac.IsGranted(ctx, "role-a", pC) &&
	rbac.IsGranted(ctx, "role-a", pD) {
	fmt.Println("The role-a has been granted permis-a, b, c and d.")
}
```

Conditional Filters (Data Scope)
--------------------------------

This repo also includes an optional CEL -> SQL filter engine for row-level data scoping:

- `github.com/fy0/gorbac/v3/filter`: a CEL -> SQL filter engine (ported from `memos`).
- Helpers in package `gorbac` to attach per-permission CEL filters (`FilterPermission`) and combine them across roles (`FilterExprsForRoles`, `NewFilterProgramFromCEL`).
- Optional: AND an extra CEL filter via `filter.WithExtraFilterCEL(...)` (useful for user query/search filters).

At a high level, permissions still decide whether a role is granted, and the attached
filter decides which rows are accessible for that permission. `FilterExprsForRoles`
does not perform permission checks; it only selects which filter expressions to
compose based on the required filter permission IDs. Missing filters are treated
as allow-all.

The filter engine supports (subset):

- Scalar columns (`FieldKindScalar`), including comparisons (`==`, `!=`, `<`, ...), `in`, and string helpers (`contains`, `startsWith`, `endsWith`)
- JSON boolean fields (`FieldKindJSONBool`)
- JSON string lists with membership + comprehensions (`FieldKindJSONList`, e.g. `"foo" in tags`, `tags.exists(t, t.contains(q))`, `t.startsWith(q)`)
- Extension hooks: register custom CEL macros/env options (`filter.WithEnvOptions`, `filter.WithMacros`) and rewrite the compiled condition tree (`filter.WithCompileHook`)
- Custom predicates: register dialect-aware SQL snippets and reference them from CEL via `sql("name", [...])` (`filter.WithSQLPredicate`)

Tip: if your schema matches a Go struct, you can build it via `filter.SchemaFromStruct(...)`.

Utility Functions
-----------------

goRBAC provides several built-in utility functions:

### InherCircle
Detects circular inheritance in the role hierarchy:

```go
rbac.SetParents(ctx, "role-c", "role-a")
if err := gorbac.InherCircle(ctx, rbac); err != nil {
	fmt.Println("A circle inheritance occurred.")
}
```

### AnyGranted
Checks if the role set grants any of the required permissions:

```go
roles := []string{"role-a", "role-b", "role-c"}
if gorbac.AnyGranted(ctx, rbac, roles, pA, pB) {
	fmt.Println("The role set grants at least one of permission-a or permission-b.")
}
```

### AllGranted
Checks if the role set grants all required permissions:

```go
roles := []string{"role-a", "role-b", "role-c"}
if gorbac.AllGranted(ctx, rbac, roles, pA, pB) {
	fmt.Println("The role set covers both permission-a and permission-b.")
}
```

### Walk
Iterates through all roles in the RBAC instance:

```go
handler := func(r gorbac.Role[string], parents []string) error {
	fmt.Printf("Role: %s, Parents: %v\n", r.ID(), parents)
	return nil
}
gorbac.Walk(ctx, rbac, handler)
```

Custom Types
------------

goRBAC supports custom types for role and permission IDs through Go generics:

```go
// Using integer IDs
rbacInt := gorbac.New[int]()
role1 := gorbac.NewRole(1)
permission1 := gorbac.NewPermission(100)

// Using custom struct IDs
type RoleID struct {
	Name string
	Type string
}

rbacStruct := gorbac.New[RoleID]()
roleCustom := gorbac.NewRole(RoleID{Name: "admin", Type: "system"})
permissionCustom := gorbac.NewPermission(RoleID{Name: "read", Type: "data"})
```

Persistence
-----------

The most asked question is how to persist the goRBAC instance. Please check the post [HOW TO PERSIST GORBAC INSTANCE](https://mikespook.com/2017/04/how-to-persist-gorbac-instance/) for the details.


Authors
=======

 * Xing Xing <mikespook@gmail.com> [Blog](http://mikespook.com) 
[@Twitter](http://twitter.com/mikespook)

Open Source - MIT Software License
==================================

See LICENSE.
