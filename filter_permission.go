package gorbac

// FilterPermission is a Permission with an attached CEL filter expression.
//
// The filter is intended for data-scope filtering (row-level filtering). The RBAC
// permission check remains unchanged: the permission ID still decides whether a
// role is granted; the filter decides which rows are accessible.
type FilterPermission[T comparable] struct {
	StdPermission[T]

	// Filter is a raw CEL boolean expression.
	//
	// It can reference:
	//   - schema fields (rendered as SQL columns)
	//   - extra CEL variables provided at runtime via filter.Bindings (rendered as placeholders)
	Filter string `json:"filter,omitempty"`
}

func NewFilterPermission[T comparable](id T, celExpr string) FilterPermission[T] {
	return FilterPermission[T]{
		StdPermission: StdPermission[T]{SID: id},
		Filter:        celExpr,
	}
}

// CEL returns the attached CEL expression.
func (p FilterPermission[T]) CEL() (string, error) {
	return p.Filter, nil
}
