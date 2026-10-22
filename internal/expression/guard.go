package expression

import (
	"errors"
	"fmt"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
)

// guardPass runs one compile pass (a walk of ours, or a stage of Expr's)
// and turns a panic in it into a type error: an expression an edit surface
// submits must be refused with what was refused, never crash the server or
// surface a nil pointer and a stack. The pass records the node it is on in
// p.at (and a pass nested in it, its own name in p.name), so the refusal
// names that construct at its position; a pass that panics before it
// reaches a node is placed on the whole expression.
func guardPass(source, name string, run func(p *pass) error) (err error) {
	p := &pass{name: name}
	defer func() {
		if r := recover(); r != nil {
			err = passRefusal(source, p.name, p.at, fmt.Sprint(r))
		}
	}()
	return run(p)
}

// pass is where a compile pass is: its name and the node it is visiting.
type pass struct {
	name string
	at   ast.Node
}

// fromCompilerPanic is the refusal for a panic Expr's compiler recovered on
// its own (it returns the panic and a stack as a plain error), or nil when
// err is not one.
func fromCompilerPanic(err error) error {
	var fe *file.Error
	if err == nil || errors.As(err, &fe) {
		return nil
	}
	msg, stack, ok := strings.Cut(err.Error(), "\ngoroutine ")
	if !ok || !strings.Contains(stack, "[running]") {
		return nil
	}
	return passRefusal("", "Expr compiler", nil, msg)
}

func passRefusal(source, pass string, at ast.Node, cause string) *Error {
	if at == nil {
		return &Error{Phase: PhaseType, Line: 1, Column: 1, Message: fmt.Sprintf(
			"the %s pass cannot compile this expression (%s); rewrite it, and report it as an engine bug", pass, cause)}
	}
	fe := (&file.Error{Location: at.Location(), Message: fmt.Sprintf(
		"the %s pass cannot compile %s here (%s); rewrite it, and report it as an engine bug", pass, construct(at), cause)}).Bind(file.NewSource(source))
	return fromExpr(PhaseType, fe)
}

// construct names a node for an operator: its kind, and its source form when
// that holds none of the engine's own plumbing (a charge it was wrapped in).
func construct(n ast.Node) string {
	kind := "an expression"
	switch m := n.(type) {
	case *ast.MemberNode:
		kind = "an index"
		if _, ok := m.Property.(*ast.StringNode); ok {
			kind = "a field read"
		}
	case *ast.BuiltinNode:
		kind = "a call to " + m.Name
	case *ast.CallNode:
		kind = "a call"
	case *ast.BinaryNode:
		kind = "the operator " + m.Operator
	case *ast.UnaryNode:
		kind = "the operator " + m.Operator
	case *ast.PredicateNode:
		kind = "a predicate"
	case *ast.IdentifierNode:
		kind = "the name"
	case *ast.ConditionalNode:
		kind = "a conditional"
	case *ast.VariableDeclaratorNode:
		kind = "a let"
	}
	if s := n.String(); !strings.Contains(s, "$") {
		return fmt.Sprintf("%s %q", kind, s)
	}
	return kind
}
