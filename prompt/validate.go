package prompt

import (
	"fmt"
	"reflect"
	"text/template/parse"
)

// fieldValidator checks the field chains a template names against the
// type of the data its section renders with.
//
// It reports an error only where it can prove the access fails: a field
// or method name the type does not have, reached through a chain it
// models. A type it cannot know — an interface, a variable, a map's key
// or element, the result of a function or a method — leaves the chain
// alone, because the parse tree does not say what it holds and the run
// reports the failure instead.
type fieldValidator struct{}

// validateTemplate walks a compiled template and returns the first field
// chain its data type provably does not have.
//
// Only the template's own tree is checked. A {{define}} body is a
// callee: the tree does not say what data the {{template}} call passes
// it, so its fields are unknown here.
func validateTemplate(tmpl *Template, data any) error {
	if tmpl.tmpl.Tree == nil || tmpl.tmpl.Tree.Root == nil {
		return nil
	}
	v := &fieldValidator{}
	if err := v.walk(tmpl.tmpl.Tree.Root, reflect.TypeOf(data)); err != nil {
		return fmt.Errorf("prompt: the section %q: %w", tmpl.name, err)
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
		if err := v.walk(n.List, rangeElement(typ)); err != nil {
			return err
		}
		return v.walk(n.ElseList, dot)
	case *parse.WithNode:
		typ, err := v.pipe(n.Pipe, dot)
		if err != nil {
			return err
		}
		if err := v.walk(n.List, indirect(typ)); err != nil {
			return err
		}
		return v.walk(n.ElseList, dot)
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
	return last, nil
}

// command checks a command's arguments and returns the type of its
// first one, unless it is a function call.
func (v *fieldValidator) command(command *parse.CommandNode, dot reflect.Type) (reflect.Type, error) {
	if len(command.Args) == 0 {
		return nil, nil
	}
	first := command.Args[0]
	for _, arg := range command.Args[1:] {
		if _, err := v.arg(arg, dot); err != nil {
			return nil, err
		}
	}
	if _, ok := first.(*parse.IdentifierNode); ok {
		// A function's result is the function's own business.
		return nil, nil
	}
	return v.arg(first, dot)
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
		return v.fieldType(base, n.Field)
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
		if typ == nil || typ.Kind() == reflect.Interface {
			return nil, nil
		}
		// A method's result is the method's own business.
		if hasMethod(typ, field) {
			return nil, nil
		}
		base := indirect(typ)
		if base == nil || base.Kind() == reflect.Interface {
			return nil, nil
		}
		switch base.Kind() {
		case reflect.Struct:
			member, ok := base.FieldByName(field)
			if !ok {
				return nil, fmt.Errorf("the template names the field %q, which %s does not have", field, typ)
			}
			if member.PkgPath != "" {
				return nil, fmt.Errorf("the template names the field %q, which %s does not export", field, typ)
			}
			typ = member.Type
		case reflect.Map:
			// A map's field chain is a key lookup, and the key is
			// the run's to choose.
			return nil, nil
		default:
			return nil, fmt.Errorf("the template names the field %q on %s, which has no fields", field, typ)
		}
	}
	return typ, nil
}

// hasMethod reports whether the type, or a pointer to it, has a method
// of that name. A method call's result is unknown, so a name matches
// either way.
func hasMethod(typ reflect.Type, name string) bool {
	if typ == nil {
		return false
	}
	if _, ok := typ.MethodByName(name); ok {
		return true
	}
	base := indirect(typ)
	if base == nil {
		return false
	}
	if _, ok := base.MethodByName(name); ok {
		return true
	}
	_, ok := reflect.PointerTo(base).MethodByName(name)
	return ok
}

// rangeElement returns the type a range's body sees.
//
// A map's element is unknown: the tree does not say how a run fills the
// map, and its key is the run's to choose.
func rangeElement(typ reflect.Type) reflect.Type {
	typ = indirect(typ)
	if typ == nil {
		return nil
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Chan:
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
