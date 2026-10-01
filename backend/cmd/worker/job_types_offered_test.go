package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Jobs page type filter renders from database.JobTypes. Before that list
// existed the dropdown was a hand-written set of four options, which omitted
// release_monitor - the scheduler's own job type, 39 of them on the playtest
// instance - so those jobs could not be filtered to at all.
//
// This is the guard the other half of that fix needs: a case added to
// runMonolithicJob must have a row in database.JobTypes, or the build fails
// here instead of shipping a type no operator can filter to. Same reasoning as
// the stylesheet-coverage guard in the templates package.
func TestEveryDispatchedJobTypeIsOfferedByTheFilter(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)

	var dispatch *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runMonolithicJob" {
			continue
		}
		dispatch = fn
		break
	}
	require.NotNil(t, dispatch, "runMonolithicJob not found")

	var dispatched []string
	ast.Inspect(dispatch.Body, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok {
				continue
			}
			if value, err := strconv.Unquote(lit.Value); err == nil {
				dispatched = append(dispatched, value)
			}
		}
		return true
	})

	require.NotEmpty(t, dispatched, "the switch should still dispatch job types")
	for _, jobType := range dispatched {
		assert.True(t, database.IsKnownJobType(jobType),
			"the worker dispatches %q but the Jobs filter does not offer it; "+
				"add a row to database.JobTypes", jobType)
	}
}
