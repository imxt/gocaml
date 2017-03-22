// Package parser provides a parsing function for GoCaml.
package parser

import (
	"bytes"
	"fmt"
	"github.com/rhysd/gocaml/ast"
	"github.com/rhysd/gocaml/token"
)

type pseudoLexer struct {
	tokens       chan token.Token
	errorCount   int
	errorMessage bytes.Buffer
	result       ast.Expr
}

func (l *pseudoLexer) Lex(lval *yySymType) int {
	for {
		select {
		case t := <-l.tokens:
			lval.token = &t

			switch t.Kind {
			case token.EOF:
				// Zero means input ends
				// (see golang.org/x/tools/cmd/goyacc/testdata/expr/expr.y)
				return 0
			case token.COMMENT:
				continue
			case token.ILLEGAL:
				return 1
			}

			// XXX:
			// Converting token value into yacc's token.
			// This conversion requires that token order must the same as
			// yacc's token order. EOF is a first token. So we can use it
			// to make an offset between token value and yacc's token value.
			return int(t.Kind) + ILLEGAL
		}
	}
}

func (l *pseudoLexer) Error(msg string) {
	l.errorCount++
	l.errorMessage.WriteString(fmt.Sprintf("  * %s\n", msg))
}

func (l *pseudoLexer) getError() error {
	return fmt.Errorf("%d error(s) while parsing\n%s", l.errorCount, l.errorMessage.String())
}

// Parse parses given tokens and returns parsed AST.
// Tokens are passed via channel.
func Parse(tokens chan token.Token) (ast.Expr, error) {
	yyErrorVerbose = true

	l := &pseudoLexer{tokens: tokens}
	ret := yyParse(l)

	if ret != 0 || l.errorCount != 0 {
		return nil, l.getError()
	}

	root := l.result
	if root == nil {
		return nil, fmt.Errorf("Parsing failed")
	}

	return root, nil
}
