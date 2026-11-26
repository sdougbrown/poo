package bash

import (
	"encoding/json"
	"strings"

	"github.com/sdougbrown/poo"
	"mvdan.cc/sh/v3/syntax"
)

// BackgroundShorthand translates a single asynchronous list into a job request.
func (t *Tool) BackgroundShorthand(raw json.RawMessage) (json.RawMessage, error) {
	var args map[string]json.RawMessage
	var command string
	if json.Unmarshal(raw, &args) != nil || json.Unmarshal(args["command"], &command) != nil {
		return nil, nil
	}
	if !strings.Contains(command, "&") {
		return nil, nil
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		// Bash still owns syntax errors and syntax the parser does not understand.
		return nil, nil
	}
	stmt := file.Stmts[len(file.Stmts)-1]
	if !stmt.Background {
		return nil, nil
	}
	if len(file.Stmts) != 1 || !canBackgroundList(stmt) {
		return nil, poo.InvalidArgsf(`bash: cannot turn this trailing & into a job; put the work in a separate call with "background": true and omit the trailing &`)
	}
	operator := int(stmt.Semicolon.Offset())
	withoutOperator := *stmt
	withoutOperator.Semicolon = syntax.Pos{}
	end := int(withoutOperator.End().Offset())
	if strings.Trim(command[end:operator], " \t") != "" {
		end = operator
	}
	trail := command[operator+1:]
	if strings.TrimSpace(trail) == "" {
		trail = ""
	} else if strings.HasPrefix(trail, "#") {
		// The operator itself can delimit a comment.
		trail = " " + trail
	}
	effective := command[:end] + trail
	args["command"], _ = json.Marshal(effective)
	return json.Marshal(args)
}

func canBackgroundList(stmt *syntax.Stmt) bool {
	ok := true
	syntax.Walk(stmt, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Stmt:
			if n.Background && n != stmt {
				ok = false
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				ok = false
				break
			}
			switch n.Args[0].Lit() {
			case "nohup", "disown", "setsid":
				ok = false
			}
		case *syntax.BinaryCmd:
		case syntax.Command, *syntax.CmdSubst, *syntax.ProcSubst:
			ok = false
		case *syntax.Redirect:
			if n.Hdoc != nil {
				ok = false
			}
		}
		return ok
	})
	return ok
}
