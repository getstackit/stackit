package cli_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/cli"
)

// suggestionHelpers are the styling/tip helpers that wrap user-facing command
// suggestions (e.g. output.Cyan("stackit restack")). Command suggestions are
// reliably routed through one of these, which lets us distinguish an actual
// "you may want to run X" hint from the many places "stackit" appears as a
// noun in prose. If a new helper starts wrapping command suggestions, add its
// (unqualified) function name here so the guard keeps covering it.
var suggestionHelpers = map[string]bool{
	"Cyan":       true, // output.Cyan
	"ColorCyan":  true, // style.ColorCyan
	"ColorGreen": true, // style.ColorGreen
	"Tip":        true, // ctx.Output.Tip (first arg is the format string)
}

var commandTokenRe = regexp.MustCompile(`^[a-z][a-z-]*$`)

// TestCommandSuggestionsResolveToRealCommands is a regression guard for #1299:
// a "fallen behind" hint suggested "stackit upstack restack" and "stackit
// stack restack", neither of which is a real command (the real form is
// "stackit restack --upstack"). init.go likewise suggested "stackit agents
// install" instead of "stackit agent install".
//
// It parses every non-test .go file under internal/ and apps/, finds string
// literals passed to a known suggestion helper that begin with "stackit ", and
// asserts the command path resolves against the live cobra tree. This way the
// suggestions can't drift away from the actual command structure as commands
// are added, renamed, or removed.
//
// Scope: it validates the leading command path (the part before any flag or
// argument placeholder). It deliberately does not validate flags or deeper
// positional arguments, since those can't be told apart from subcommands
// statically.
func TestCommandSuggestionsResolveToRealCommands(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCmd("test", "test", "test")
	repoRoot := findRepoRoot(t)

	type violation struct {
		file       string
		suggestion string
		token      string
	}
	var violations []violation
	checked := 0

	for _, sub := range []string{"internal", "apps"} {
		base := filepath.Join(repoRoot, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, parseErr, "failed to parse %s", path)

			rel, _ := filepath.Rel(repoRoot, path)
			ast.Inspect(file, func(n ast.Node) bool {
				suggestion, ok := suggestionLiteral(n)
				if !ok {
					return true
				}
				tokens := commandTokens(suggestion)
				if len(tokens) == 0 {
					return true
				}
				checked++
				if !commandPathResolves(root, tokens) {
					violations = append(violations, violation{file: rel, suggestion: suggestion, token: tokens[0]})
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}

	require.NotZero(t, checked, "expected to find command suggestions to validate")

	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("found command suggestions that do not resolve to real stackit commands:\n")
		for _, v := range violations {
			b.WriteString("  " + v.file + ": " + strconv.Quote(v.suggestion) +
				" — \"" + v.token + "\" is not a stackit command\n")
		}
		b.WriteString("Use a real command path (e.g. flags like \"stackit restack --upstack\", not \"stackit upstack restack\").")
		t.Fatal(b.String())
	}
}

// suggestionLiteral returns the suggestion string if n is a call to one of the
// suggestionHelpers whose first argument is a string literal starting with
// "stackit ".
func suggestionLiteral(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}

	var name string
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	case *ast.Ident:
		name = fun.Name
	default:
		return "", false
	}
	if !suggestionHelpers[name] {
		return "", false
	}

	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.HasPrefix(val, "stackit ") {
		return "", false
	}
	return val, true
}

// commandTokens extracts the leading command path from a "stackit ..."
// suggestion, stopping at the first flag, argument placeholder, or format verb.
func commandTokens(suggestion string) []string {
	rest := strings.TrimPrefix(suggestion, "stackit ")
	var tokens []string
	for _, f := range strings.Fields(rest) {
		if !commandTokenRe.MatchString(f) {
			break
		}
		tokens = append(tokens, f)
	}
	return tokens
}

// commandPathResolves reports whether the leading tokens describe a real
// command path. The first token must be a subcommand of root; once a token
// stops matching, the remainder is treated as arguments.
func commandPathResolves(root *cobra.Command, tokens []string) bool {
	node := root
	for i, tok := range tokens {
		child := findChild(node, tok)
		if child == nil {
			return i > 0
		}
		node = child
	}
	return true
}

func findChild(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name {
			return c
		}
		for _, alias := range c.Aliases {
			if alias == name {
				return c
			}
		}
	}
	return nil
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "could not determine test file path")
	// This file lives at internal/cli/, so the repo root is two levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
