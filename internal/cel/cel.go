// Package cel provides CEL (Common Expression Language) utilities for filtering
// audit logs and activities in ClickHouse queries.
//
// This package implements a shared infrastructure for:
//   - Compiling CEL filter expressions with field validation
//   - Converting CEL ASTs to ClickHouse SQL WHERE clauses
//   - Domain-specific field mapping (audit logs, activities)
//
// The design uses interfaces to allow different domains (audit logs, activities)
// to share the common CEL parsing and SQL generation logic while customizing
// field validation and column mapping.
package cel

import (
	"fmt"
	"strings"
	"time"

	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// FieldValidator defines the interface for validating CEL field access.
// Each domain implements this to define which fields are allowed in filter expressions.
type FieldValidator interface {
	// ValidateSelectExpr validates a field selection expression.
	// Returns nil if the field access is valid, or an error describing the issue.
	ValidateSelectExpr(sel *expr.Expr_Select) error
}

// FieldMapper defines the interface for mapping CEL fields to ClickHouse columns.
// Each domain implements this to define how CEL expression fields map to their
// ClickHouse table columns.
type FieldMapper interface {
	// MapSelectExpr converts a CEL select expression to a ClickHouse column name.
	// It receives the full select expression to handle both simple (objectRef.namespace)
	// and nested (spec.actor.name) field access patterns.
	MapSelectExpr(sel *expr.Expr_Select) (string, error)

	// MapIdentExpr handles bare identifiers (e.g., "auditID", "verb").
	// Returns an error if the identifier requires dot notation access.
	MapIdentExpr(ident *expr.Expr_Ident) (string, error)
}

// ArrayFieldMapper is optionally implemented by a FieldMapper with ClickHouse
// Array columns. Array fields are only valid on the right of `in`, which
// converts to has().
type ArrayFieldMapper interface {
	// MapArraySelectExpr returns the Array column for sel, or false if sel is
	// not an array field.
	MapArraySelectExpr(sel *expr.Expr_Select) (string, bool)
}

// ValidateFieldAccess recursively validates that only allowed fields are accessed
// in a CEL expression. It uses the provided FieldValidator for domain-specific
// field validation.
func ValidateFieldAccess(e *expr.Expr, validator FieldValidator) error {
	if e == nil {
		return nil
	}

	switch exprKind := e.ExprKind.(type) {
	case *expr.Expr_SelectExpr:
		sel := exprKind.SelectExpr
		if err := validator.ValidateSelectExpr(sel); err != nil {
			return err
		}
		// Recursively validate the operand
		if operand := sel.GetOperand(); operand != nil {
			if err := ValidateFieldAccess(operand, validator); err != nil {
				return err
			}
		}

	case *expr.Expr_CallExpr:
		call := exprKind.CallExpr
		if call.Target != nil {
			if err := ValidateFieldAccess(call.Target, validator); err != nil {
				return err
			}
		}
		for _, arg := range call.Args {
			if err := ValidateFieldAccess(arg, validator); err != nil {
				return err
			}
		}

	case *expr.Expr_ListExpr:
		list := exprKind.ListExpr
		for _, elem := range list.Elements {
			if err := ValidateFieldAccess(elem, validator); err != nil {
				return err
			}
		}

	case *expr.Expr_StructExpr:
		structExpr := exprKind.StructExpr
		for _, entry := range structExpr.Entries {
			if err := ValidateFieldAccess(entry.GetValue(), validator); err != nil {
				return err
			}
		}

	case *expr.Expr_ComprehensionExpr:
		comp := exprKind.ComprehensionExpr
		if err := ValidateFieldAccess(comp.IterRange, validator); err != nil {
			return err
		}
		if err := ValidateFieldAccess(comp.AccuInit, validator); err != nil {
			return err
		}
		if err := ValidateFieldAccess(comp.LoopCondition, validator); err != nil {
			return err
		}
		if err := ValidateFieldAccess(comp.LoopStep, validator); err != nil {
			return err
		}
		if err := ValidateFieldAccess(comp.Result, validator); err != nil {
			return err
		}
	}

	return nil
}

// BaseSQLConverter contains shared CEL-to-ClickHouse SQL conversion logic.
// It handles operator conversion, constant handling, and parameter management.
// Domain-specific field mapping is delegated to the FieldMapper interface.
type BaseSQLConverter struct {
	args      []any
	argIndex  int
	paramName map[int]string
	mapper    FieldMapper
}

// NewBaseSQLConverter creates a new BaseSQLConverter with the given field mapper.
func NewBaseSQLConverter(mapper FieldMapper) *BaseSQLConverter {
	return &BaseSQLConverter{
		args:      make([]any, 0),
		argIndex:  1,
		paramName: make(map[int]string),
		mapper:    mapper,
	}
}

// Args returns the collected query arguments.
func (c *BaseSQLConverter) Args() []any {
	return c.args
}

// addArg adds a value to the argument list and returns a ClickHouse parameter placeholder.
func (c *BaseSQLConverter) addArg(value any) string {
	c.args = append(c.args, value)
	paramName := fmt.Sprintf("arg%d", c.argIndex)
	c.paramName[c.argIndex] = paramName
	c.argIndex++
	return fmt.Sprintf("{%s}", paramName)
}

// ConvertExpr converts a CEL expression to a ClickHouse SQL string.
func (c *BaseSQLConverter) ConvertExpr(e *expr.Expr) (string, error) {
	switch e.ExprKind.(type) {
	case *expr.Expr_CallExpr:
		return c.convertCallExpr(e.GetCallExpr())
	case *expr.Expr_IdentExpr:
		return c.mapper.MapIdentExpr(e.GetIdentExpr())
	case *expr.Expr_ConstExpr:
		return c.convertConstExpr(e.GetConstExpr())
	case *expr.Expr_SelectExpr:
		return c.mapper.MapSelectExpr(e.GetSelectExpr())
	case *expr.Expr_ListExpr:
		return c.convertListExpr(e.GetListExpr())
	default:
		return "", fmt.Errorf("unsupported expression type: %T", e.ExprKind)
	}
}

func (c *BaseSQLConverter) convertCallExpr(call *expr.Expr_Call) (string, error) {
	switch call.Function {
	case "!_":
		arg, err := c.ConvertExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("NOT (%s)", arg), nil

	case "_==_":
		return c.convertBinaryOp(call, "=")

	case "_!=_":
		return c.convertBinaryOp(call, "!=")

	case "_&&_":
		left, err := c.ConvertExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		right, err := c.ConvertExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s AND %s)", left, right), nil

	case "_||_":
		left, err := c.ConvertExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		right, err := c.ConvertExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s OR %s)", left, right), nil

	case "_>=_":
		return c.convertBinaryOp(call, ">=")

	case "_<=_":
		return c.convertBinaryOp(call, "<=")

	case "_>_":
		return c.convertBinaryOp(call, ">")

	case "_<_":
		return c.convertBinaryOp(call, "<")

	case "@in":
		left, err := c.ConvertExpr(call.Args[0])
		if err != nil {
			return "", err
		}
		if column, ok := c.arrayColumn(call.Args[1]); ok {
			return fmt.Sprintf("has(%s, %s)", column, left), nil
		}
		right, err := c.ConvertExpr(call.Args[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s IN %s", left, right), nil

	case "startsWith":
		if call.Target != nil {
			target, err := c.ConvertExpr(call.Target)
			if err != nil {
				return "", err
			}
			prefix, err := c.ConvertExpr(call.Args[0])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("startsWith(%s, %s)", target, prefix), nil
		}

	case "endsWith":
		if call.Target != nil {
			target, err := c.ConvertExpr(call.Target)
			if err != nil {
				return "", err
			}
			suffix, err := c.ConvertExpr(call.Args[0])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("endsWith(%s, %s)", target, suffix), nil
		}

	case "contains":
		if call.Target != nil {
			target, err := c.ConvertExpr(call.Target)
			if err != nil {
				return "", err
			}
			substring, err := c.ConvertExpr(call.Args[0])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("position(%s, %s) > 0", target, substring), nil
		}

	case "timestamp":
		if len(call.Args) == 1 {
			if constExpr := call.Args[0].GetConstExpr(); constExpr != nil {
				if strVal := constExpr.GetStringValue(); strVal != "" {
					t, err := time.Parse(time.RFC3339, strVal)
					if err != nil {
						return "", fmt.Errorf("invalid timestamp format: %w", err)
					}
					return c.addArg(t), nil
				}
			}
		}
	}

	return "", fmt.Errorf("unsupported CEL function: %s", call.Function)
}

func (c *BaseSQLConverter) arrayColumn(e *expr.Expr) (string, bool) {
	sel := e.GetSelectExpr()
	if sel == nil {
		return "", false
	}
	arrayMapper, ok := c.mapper.(ArrayFieldMapper)
	if !ok {
		return "", false
	}
	return arrayMapper.MapArraySelectExpr(sel)
}

func (c *BaseSQLConverter) convertBinaryOp(call *expr.Expr_Call, op string) (string, error) {
	left, err := c.ConvertExpr(call.Args[0])
	if err != nil {
		return "", err
	}
	right, err := c.ConvertExpr(call.Args[1])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", left, op, right), nil
}

func (c *BaseSQLConverter) convertConstExpr(constant *expr.Constant) (string, error) {
	switch constant.ConstantKind.(type) {
	case *expr.Constant_StringValue:
		return c.addArg(constant.GetStringValue()), nil
	case *expr.Constant_Int64Value:
		return c.addArg(constant.GetInt64Value()), nil
	case *expr.Constant_Uint64Value:
		return c.addArg(constant.GetUint64Value()), nil
	case *expr.Constant_DoubleValue:
		return c.addArg(constant.GetDoubleValue()), nil
	case *expr.Constant_BoolValue:
		if constant.GetBoolValue() {
			return "1", nil
		}
		return "0", nil
	default:
		return "", fmt.Errorf("unsupported constant type: %T", constant.ConstantKind)
	}
}

func (c *BaseSQLConverter) convertListExpr(list *expr.Expr_CreateList) (string, error) {
	elements := make([]string, len(list.Elements))
	for i, elem := range list.Elements {
		val, err := c.ConvertExpr(elem)
		if err != nil {
			return "", err
		}
		elements[i] = val
	}
	return fmt.Sprintf("[%s]", strings.Join(elements, ", ")), nil
}
