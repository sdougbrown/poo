package prompt

import (
	"fmt"
	"reflect"
	"text/template/parse"
)

// fieldValidator checks the field chains a template names against the
// type of the data its section renders with.
//
// A chain is checked in every branch, not only the branches the
// validation environment happens to take. A type the validator cannot
// know — an interface, or the result of a function — leaves the chain
// alone: the parse tree does not say what it holds.
type fieldValidator struct {
	// vars are the variables the template declares, by name.
	vars map[string]reflect.Type
}

// validateTemplate walks a compiled template and returns the first field
// chain its data type does not have.
func validateTemplate(tmpl *Template, data any) error {
	root := reflect.TypeOf(data)
	for _, associated := range tmpl.tmpl.Templates() {
		if associated.Tree == nil || associated.Tree.Root == nil {
			continue
		}
		v := &fieldValidator{vars: map[string]reflect.Type{"$": root}}
		if err := v.walk(associated.Tree.Root, root); err != nil {
			return fmt.Errorf("prompt: the section %q: %w", tmpl.name, err)
		}
	}
	return nil
}

// walk checks one node of the parse tree.
func (v *fieldValidator) walk(node parse.Node, dot reflect.Type) error {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, child := range n.Nodes {
			if err := v.walk(child, dot); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		_, err := v.pipe(n.Pipe, dot)
		return err
	case *parse.IfNode:
		if _, err := v.pipe(n.Pipe, dot); err != nil {
			return err
		}
		if err := v.walk(n.List, dot); err != nil {
			return err
		}
		return v.walk(n.ElseList, dot)
	case *parse.RangeNode:
		typ, err := v.pipe(n.Pipe, dot)
		if err != nil {
			return err
		}
		bodyDot := rangeElement(typ)
		saved := v.vars
		v.vars = cloneTypes(saved)
		v.declareRange(n.Pipe, bodyDot)
		err = v.walk(n.List, bodyDot)
		v.vars = saved
		if err != nil {
			return err
		}
		return v.walk(n.ElseList, dot)
	case *parse.WithNode:
		typ, err := v.pipe(n.Pipe, dot)
		if err != nil {
			return err
		}
		saved := v.vars
		v.vars = cloneTypes(saved)
		v.declareWith(n.Pipe, typ)
		err = v.walk(n.List, typ)
		v.vars = saved
		if err != nil {
			return err
		}
		return v.walk(n.ElseList, dot)
	case *parse.TemplateNode:
		if n.Pipe == nil {
			return nil
		}
		_, err := v.pipe(n.Pipe, dot)
		return err
	}
	return nil
}

// pipe checks the commands of a pipeline and returns the type of its
// last command, when the validator knows it.
func (v *fieldValidator) pipe(pipe *parse.PipeNode, dot reflect.Type) (reflect.Type, error) {
	if pipe == nil {
		return nil, nil
	}
	var last reflect.Type
	for _, command := range pipe.Cmds {
		typ, err := v.command(command, dot)
		if err != nil {
			return nil, err
		}
		last = typ
	}
	if pipe.Decl != nil && last != nil {
		for _, declaration := range pipe.Decl {
			if len(declaration.Ident) > 0 {
				v.vars[declaration.Ident[0]] = last
			}
		}
	}
	return last, nil
}

// command checks a command's arguments and returns the type of its
// first one.
func (v *fieldValidator) command(command *parse.CommandNode, dot reflect.Type) (reflect.Type, error) {
	if len(command.Args) == 0 {
		return nil, nil
	}
	first := command.Args[0]
	typ, err := v.arg(first, dot)
	if err != nil {
		return nil, err
	}
	for _, arg := range command.Args[1:] {
		if _, err := v.arg(arg, dot); err != nil {
			return nil, err
		}
	}
	if _, ok := first.(*parse.IdentifierNode); ok {
		// A function's result is the function's own business.
		return nil, nil
	}
	return typ, nil
}

// arg checks one argument and returns its type, when the validator
// knows it.
func (v *fieldValidator) arg(node parse.Node, dot reflect.Type) (reflect.Type, error) {
	switch n := node.(type) {
	case *parse.FieldNode:
		return v.fieldType(dot, n.Ident)
	case *parse.ChainNode:
		base, err := v.arg(n.Node, dot)
		if err != nil {
			return nil, err
		}
		if base == nil {
			return nil, nil
		}
		return v.fieldType(base, n.Field)
	case *parse.VariableNode:
		if len(n.Ident) == 0 {
			return nil, nil
		}
		typ := v.vars[n.Ident[0]]
		if len(n.Ident) == 1 || typ == nil {
			return typ, nil
		}
		return v.fieldType(typ, n.Ident[1:])
	case *parse.PipeNode:
		return v.pipe(n, dot)
	case *parse.DotNode:
		return dot, nil
	case *parse.BoolNode:
		return reflect.TypeOf(false), nil
	case *parse.NumberNode:
		return reflect.TypeOf(0), nil
	case *parse.StringNode:
		return reflect.TypeOf(""), nil
	}
	return nil, nil
}

// fieldType walks a field chain from a type and returns the type it
// ends at.
func (v *fieldValidator) fieldType(typ reflect.Type, fields []string) (reflect.Type, error) {
	for _, field := range fields {
		typ = indirect(typ)
		if typ == nil || typ.Kind() == reflect.Interface {
			return nil, nil
		}
		switch typ.Kind() {
		case reflect.Struct:
			if member, ok := typ.FieldByName(field); ok {
				typ = member.Type
				continue
			}
			if _, ok := typ.MethodByName(field); ok {
				return nil, nil
			}
			return nil, fmt.Errorf("the template names the field %q, which %s does not have", field, typ)
		case reflect.Map:
			typ = typ.Elem()
			continue
		default:
			if _, ok := typ.MethodByName(field); ok {
				return nil, nil
			}
			return nil, fmt.Errorf("the template names the field %q on %s, which has no fields", field, typ)
		}
	}
	return typ, nil
}

// declareRange binds a range's variables: the index and the element.
func (v *fieldValidator) declareRange(pipe *parse.PipeNode, element reflect.Type) {
	if pipe == nil || pipe.Decl == nil {
		return
	}
	for i, declaration := range pipe.Decl {
		if len(declaration.Ident) == 0 {
			continue
		}
		if i == len(pipe.Decl)-1 {
			v.vars[declaration.Ident[0]] = element
			continue
		}
		v.vars[declaration.Ident[0]] = reflect.TypeOf(0)
	}
}

// declareWith binds a with block's variable to the pipeline's result.
func (v *fieldValidator) declareWith(pipe *parse.PipeNode, typ reflect.Type) {
	if pipe == nil || pipe.Decl == nil {
		return
	}
	for _, declaration := range pipe.Decl {
		if len(declaration.Ident) > 0 {
			v.vars[declaration.Ident[0]] = typ
		}
	}
}

// rangeElement returns the type a range's body sees.
func rangeElement(typ reflect.Type) reflect.Type {
	typ = indirect(typ)
	if typ == nil {
		return nil
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Chan, reflect.Map:
		return typ.Elem()
	case reflect.String:
		return reflect.TypeOf("")
	}
	return nil
}

// indirect follows a pointer to the type it points at.
func indirect(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// cloneTypes copies a variable table, so a block may bind its own names
// without changing the table around it.
func cloneTypes(vars map[string]reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, len(vars))
	for name, typ := range vars {
		out[name] = typ
	}
	return out
}
