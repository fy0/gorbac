# goRBAC - LLM Coder Guide

This document provides a comprehensive guide to the goRBAC (Role-Based Access Control) package for LLM coders who need to understand and work with this Go library without going through the entire codebase.

## Overview

goRBAC is a lightweight role-based access control implementation in Golang. It provides a simple and efficient way to manage roles, permissions, and their relationships in applications that require access control.

### Core Concepts

1. **Identity**: An entity that has one or more roles.
2. **Role**: A named entity that can be assigned permissions and can inherit from parent roles.
3. **Permission**: An entity that represents an action or resource access right.
4. **Inheritance**: Roles can inherit permissions from parent roles, forming a hierarchical structure.

### Key Features

- Generic support for different ID types (string, int, etc.)
- Role inheritance with circular dependency detection
- Thread-safe operations
- JSON serialization support
- Embeddable role structs + permission interfaces for customization
- Built-in utility functions for common operations

## Package Structure

```
gorbac/
├── rbac.go              # Main RBAC implementation
├── role.go              # Role interface + StdRole helpers
├── permission.go        # Permission interface + StdPermission
├── filter_permission.go # Permission with attached CEL data-scope filter
├── filter_scope.go      # Helpers to build role-based SQL filters
├── helper.go            # Utility functions
├── helper_test.go       # Tests for helper functions
├── rbac_test.go         # Tests for RBAC implementation
├── role_test.go         # Tests for role implementation
├── permission_test.go   # Tests for permission implementation
├── example_test.go      # Usage examples
├── filter/              # CEL -> IR -> SQL filter engine (ported from memos)
├── examples/            # Complete example applications
│   ├── persistence/     # Example showing data persistence
│   └── user-defined/    # Example with custom role implementation
├── README.md            # Project documentation
└── go.mod               # Go module definition
```

## Core Components

### 1. RBAC Structure (`rbac.go`)

The `RBAC[T]` interface defines the contract, and `StdRBAC[T]` is the default implementation.

#### Key Methods

- `New[T comparable]() *StdRBAC[T]` - Creates a new RBAC instance (default implementation)
- `Add(ctx context.Context, r Role[T]) error` - Adds a role to the RBAC instance
- `Remove(ctx context.Context, id T) error` - Removes a role by ID
- `Get(ctx context.Context, id T) (Role[T], error)` - Gets a role
- `RoleIDs(ctx context.Context) []T` - Returns all role IDs
- `SetParents(ctx context.Context, id T, parents ...T) error` - Sets one or more parents for a role
- `GetParents(ctx context.Context, id T) ([]T, error)` - Gets all parents of a role
- `RemoveParents(ctx context.Context, id T, parents ...T) error` - Removes one or more parents from a role
- `IsGranted(ctx context.Context, id T, p Permission[T]) bool` - Checks if a role has a permission

#### Thread Safety

All operations on the default `StdRBAC` implementation are thread-safe using read-write mutexes.

### 2. Role Implementation (`role.go`)

The `Role[T]` interface defines the contract, and `StdRole[T]` is the default implementation.

```go
type Role[T comparable] interface {
    ID() T
    Assign(ctx context.Context, perms ...Permission[T]) error
    Permit(ctx context.Context, perms ...Permission[T]) bool
    Revoke(ctx context.Context, perms ...Permission[T]) error
    Permissions(ctx context.Context) []Permission[T]
    PermissionsMap(ctx context.Context) map[T]Permission[T]
    Get(ctx context.Context, id T) (Permission[T], bool)
    FilterPermissions(ctx context.Context) map[T]Permission[T]
}

type StdRole[T comparable] struct {
    mutex *sync.RWMutex
    IDValue T `json:"id"`
    permissions Permissions[T]
    filterPermissions map[T]Permission[T]
}
```

#### Key Methods

- `NewRole[T comparable](id T) *StdRole[T]` - Creates a new standard role
- `Assign(ctx context.Context, perms ...Permission[T]) error` - Assigns permissions to the role
- `Permit(ctx context.Context, perms ...Permission[T]) bool` - Checks if the role has all specified permissions
- `Revoke(ctx context.Context, perms ...Permission[T]) error` - Revokes permissions from the role
- `Permissions(ctx context.Context) []Permission[T]` - Returns all permissions assigned to the role
- `Get(ctx context.Context, id T) (Permission[T], bool)` - Looks up a permission by ID
- `PermissionsMap(ctx context.Context) map[T]Permission[T]` - Returns a raw map of permissions keyed by ID
- `FilterPermissions(ctx context.Context) map[T]Permission[T]` - Returns a raw map of CEL-carrying permissions keyed by ID

`PermissionsMap(ctx)` and `FilterPermissions(ctx)` return raw references. Do not mutate them directly; use `Assign(ctx, ...)`/`Revoke(ctx, ...)` to keep internal caches consistent.

### 3. Permission Interface and Implementation (`permission.go`)

The `Permission[T]` interface defines the contract for permissions:

```go
type Permission[T comparable] interface {
    ID() T
    Match(Permission[T]) bool
}
```

#### Standard Permission Implementation

The package provides `StdPermission[T]` as the default implementation:

- `SID` - Serializable ID of the permission

#### Key Methods

- `NewPermission[T comparable](id T) Permission[T]` - Creates a new permission
- `ID() T` - Returns the permission ID
- `Match(Permission[T]) bool` - Checks if this permission matches another

### 4. Helper Functions (`helper.go`)

Utility functions for common operations:

#### Walk Function

- `Walk[T comparable](ctx context.Context, rbac RBAC[T], h WalkHandler[T]) error` - Iterates through all roles

#### Inheritance Validation

- `InherCircle[T comparable](ctx context.Context, rbac RBAC[T]) error` - Detects circular inheritance

#### Permission Checking

- `AnyGranted[T comparable](ctx context.Context, rbac RBAC[T], roles []T, permissions ...Permission[T]) bool` - Checks if the role set grants any specified permission
- `AllGranted[T comparable](ctx context.Context, rbac RBAC[T], roles []T, permissions ...Permission[T]) bool` - Checks if the role set grants all specified permissions

## Usage Examples

### Basic Usage

```go
ctx := context.Background()

// Create a new RBAC instance
rbac := gorbac.New[string]()

// Create roles
rA := gorbac.NewRole("role-a")
rB := gorbac.NewRole("role-b")

// Create permissions
pA := gorbac.NewPermission("permission-a")
pB := gorbac.NewPermission("permission-b")

// Assign permissions to roles
rA.Assign(ctx, pA)
rB.Assign(ctx, pB)

// Add roles to RBAC
rbac.Add(ctx, rA)
rbac.Add(ctx, rB)

// Set inheritance
rbac.SetParents(ctx, "role-a", "role-b")

// Check permissions
if rbac.IsGranted(ctx, "role-a", pA) {
    // role-a has permission-a
}
```

### Working with Different ID Types

The package supports generic ID types:

```go
// String IDs
rbacStr := gorbac.New[string]()

// Integer IDs
rbacInt := gorbac.New[int]()

// Custom struct IDs
type RoleID struct {
    Name string
    Type string
}
rbacStruct := gorbac.New[RoleID]()
```

## Conditional Filters (Data Scope)

In addition to “permission granted” checks, this repo includes a CEL-based data-scope filter system:

- `FilterPermission[T]` (`filter_permission.go`): a permission with an attached CEL expression (string) used for row-level filtering.
- `filter` package (`filter/`): compiles CEL boolean expressions into a dialect-agnostic condition tree, which can be rendered as SQL (SQLite/MySQL/Postgres) or evaluated in-memory.
- `filter_scope.go`: glue helpers to build a single SQL fragment across all user roles (OR across roles, AND across required filter permissions).

### Concepts

1. **Schema (`filter.Schema`)**: maps CEL identifiers to SQL columns / JSON fields, including supported operators.
2. **Engine (`filter.NewEngine`)**: parses + type-checks CEL with `cel-go`, then converts CEL AST to a small intermediate representation (IR).
3. **Program**: holds the compiled IR (`ConditionTree()`), supports:
   - `RenderSQL(bindings, opts)` -> `(Statement, error)`
   - `IsCondGranted(vars, opts)` -> `(bool, error)` (in-memory counterpart)

### SQL Output: placeholders + args

SQL is always emitted as **SQL fragment + args**. For Postgres the renderer produces `$1/$2/...` placeholders.

When composing multiple fragments, use `filter.RenderOptions.PlaceholderOffset` to continue numbering:

```go
stmt1 := ...
stmt2, _ := engine.CompileToStatement(expr2, bindings, filter.RenderOptions{
    Dialect: filter.DialectPostgres,
    PlaceholderOffset: len(stmt1.Args),
})
finalSQL  := "(" + stmt1.SQL + ") AND (" + stmt2.SQL + ")"
finalArgs := append(stmt1.Args, stmt2.Args...)
```

### SQL Output: table qualifiers

By default, fields render as `table.column` using `filter.Field.Column.Table`.

When embedding the generated fragment into queries that use **aliases** (or a different qualifier),
use `filter.RenderOptions.TableAliases` to rewrite table names, or `filter.RenderOptions.OmitTableQualifier`
to render unqualified column names:

```go
stmt, _ := engine.CompileToStatement(`creator_id == current_user_id`, bindings, filter.RenderOptions{
    Dialect: filter.DialectPostgres,
    TableAliases: map[string]string{"project": "p"}, // renders "p.creator_id"
    // OmitTableQualifier: true,                    // renders "creator_id"
})
```

### Extension hooks

The filter engine is designed to be extended without forking:

- **CEL macros / env options**: `filter.WithEnvOptions(...)`, `filter.WithMacros(...)`
- **Post-compile rewrite**: `filter.WithCompileHook(...)` can replace the compiled IR tree before SQL rendering / evaluation.
- **Custom SQL predicates (subquery injection)**: `filter.WithSQLPredicate(...)` + `sql("name", [...])` in CEL

#### Custom SQL predicates (`sql("name", [...])`)

Use this when you need to inject correlated subqueries (e.g. membership checks):

```go
engine, _ := filter.NewEngine(schema,
    filter.WithSQLPredicate("project_member", filter.SQLPredicate{
        SQL: filter.DialectSQL{
            Postgres: `EXISTS (
                SELECT 1 FROM project_member pm
                WHERE pm.project_id = {{project_id}}
                  AND pm.user_id = ?::bigint
                  AND pm.status = 'ACTIVE'
            )`,
        },
    }),
)

stmt, _ := engine.CompileToStatement(
    `creator_id == current_user_id || sql("project_member", [current_user_id])`,
    filter.Bindings{"current_user_id": int64(1001)},
    filter.RenderOptions{Dialect: filter.DialectPostgres},
)
// stmt.SQL  -> "(p.creator_id = $1 OR EXISTS (... pm.user_id = $2::bigint ...))"
// stmt.Args -> [1001 1001]
```

Notes:

- `{{field_name}}` is replaced with the schema column expression for that field (dialect-aware quoting).
- `?` placeholders are converted to dialect placeholders and values are appended to `stmt.Args`.
- Templates are trusted code/config; user-provided input should go through args/bindings (placeholders), not string concatenation.

### Role-based helpers (`filter_scope.go`)

- `FilterExprsForRoles(...)` -> `[]string` (CEL expressions composed from required filter permission IDs; missing filters are treated as allow-all)
- `NewFilterProgramFromCEL(...)` -> `*filter.Program` (OR across role expressions; AND across required filter permissions happens in `FilterExprsForRoles`)
- Optional: `filter.WithExtraFilterCEL(...)` to AND an additional CEL filter (e.g. user query/search) onto the permission scope.
- The returned program can be used with:
  - `program.RenderSQL(bindings, opts)` -> `(filter.Statement, error)`
  - `program.IsCondGranted(vars, opts)` -> `(bool, error)`

## Persistence

The package doesn't include built-in persistence but provides mechanisms for implementing it:

### Example Persistence Approach

See `examples/persistence/persistence.go` for a complete example of:

1. Loading roles and permissions from JSON files
2. Building the RBAC structure from persisted data
3. Saving the RBAC structure back to JSON files

### Key Concepts for Persistence

1. Serialize roles and their permissions
2. Serialize inheritance relationships
3. Reconstruct the RBAC instance from persisted data

## Custom Implementations

### Custom Role Implementation

You can create custom roles by embedding the standard role:

```go
type myRole struct {
    gorbac.StdRole[string]  // Embed the standard role
    Label       string
    Description string
}
```

### Custom Permission Implementation

You can implement the `Permission[T]` interface to create custom permissions with additional logic in the `Match` method.

## Error Handling

The package defines standard errors:

- `ErrRoleNotExist` - When a role doesn't exist
- `ErrRoleExist` - When trying to add a role that already exists
- `ErrFoundCircle` - When circular inheritance is detected

Always check and handle these errors appropriately in your applications.

## Performance Considerations

- RBAC operations use read-write mutexes for thread safety
- Permission checking with inheritance uses recursive traversal
- Circular inheritance detection uses depth-first search
- Consider caching results for frequently checked permissions in performance-critical applications

## Testing

The package includes comprehensive tests covering:

- Basic RBAC operations
- Role and permission management
- Inheritance relationships
- Circular dependency detection
- Helper functions
- Various ID types

See the `*_test.go` files for detailed usage examples.

## Quick Reference

| Component | File | Key Functions |
|-----------|------|---------------|
| RBAC Core | `rbac.go` | `New`, `Add`, `Remove`, `IsGranted`, `SetParents` |
| Roles | `role.go` | `NewRole`, `Assign`, `Permit`, `Revoke` |
| Permissions | `permission.go` | `NewPermission`, `Match` |
| Data Scope | `filter_scope.go` | `FilterExprsForRoles`, `NewFilterProgramFromCEL` |
| Filter Engine | `filter/` | `NewEngine`, `SchemaFromStruct`, `WithMacros`, `WithSQLPredicate` |
| Utilities | `helper.go` | `Walk`, `InherCircle`, `AnyGranted`, `AllGranted` |
| Examples | `example_test.go` | Complete usage examples |

## Common Patterns

### 1. Initialization Pattern

```go
rbac := gorbac.New[string]()
// Create roles and permissions
// Assign permissions to roles
// Add roles to RBAC
// Set up inheritance
```

### 2. Permission Checking Pattern

```go
if rbac.IsGranted(ctx, "user-role", requiredPermission) {
    // Allow access
} else {
    // Deny access
}
```

### 3. Batch Permission Checking

```go
roles := []string{"role1", "role2", "role3"}
if gorbac.AnyGranted(ctx, rbac, roles, permissionA, permissionB) {
    // Role set grants at least one required permission
}

if gorbac.AllGranted(ctx, rbac, roles, permissionA, permissionB) {
    // Role set covers all required permissions
}
```

## Extending the Package

1. Embed standard `StdRole[T]` struct for domain-specific role behavior
2. Implement custom `Permission[T]` interfaces for complex permission matching logic
3. Use the `Walk` function to export RBAC state for persistence
4. Add middleware functions for logging or metrics around RBAC operations

This guide provides a comprehensive overview of the goRBAC package. For implementation details, refer to the source files in the package structure.

