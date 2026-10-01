package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// VIEW LOGS is the first place an operator goes when a job finished and they
// want to know what happened. Before this, only the acquisition path and the
// scanner's prune routine wrote job_logs, so on artist_scan, release_monitor,
// scan, enrich and index_refresh the panel said "No log entries for this job."
// about a job that had succeeded minutes earlier. That reads as data loss.
//
// Each case drives runMonolithicJob for real with a scope that cannot resolve,
// which is the fastest honest failure available without stubbing the services -
// and a failure is the case that matters most here, because AC3 is about the
// reason being visible rather than only in the container log.
func TestRunMonolithicJobRecordsLogsForEveryDispatchedType(t *testing.T) {
	tests := []struct {
		jobType string
		scopeID string
	}{
		{jobType: "artist_scan", scopeID: uuid.NewString()},
		{jobType: "release_monitor", scopeID: ""},
		{jobType: "index_refresh", scopeID: ""},
		{jobType: "scan", scopeID: "not-a-uuid"},
		{jobType: "enrich", scopeID: "not-a-uuid"},
		{jobType: "prune", scopeID: "not-a-uuid"},
	}

	for _, tc := range tests {
		t.Run(tc.jobType, func(t *testing.T) {
			w := setupWorkerTestDB(t)

			job := database.Job{
				Type:        tc.jobType,
				State:       "running",
				ScopeID:     tc.scopeID,
				RequestedAt: time.Now(),
			}
			require.NoError(t, w.db.Create(&job).Error)

			jc := &jobContext{
				job:        job,
				ctx:        context.Background(),
				processing: true,
			}
			w.jobMutex.Lock()
			w.activeJobs[job.ID] = jc
			w.jobMutex.Unlock()

			w.runMonolithicJob(jc)

			var logs []database.JobLog
			require.NoError(t, w.db.Where("job_id = ?", job.ID).Order("id").Find(&logs).Error)
			require.NotEmpty(t, logs, "VIEW LOGS would be an empty panel for every %s job", tc.jobType)

			assert.Equal(t, "INFO", logs[0].Level)
			assert.Contains(t, logs[0].Message, tc.jobType,
				"the first entry says which job started")
			assert.Contains(t, logs[0].Message, "Starting")

			last := logs[len(logs)-1]
			assert.Contains(t,
				[]string{"OK", "WARN", "ERR"}, last.Level,
				"the last entry reports a terminal level")
			assert.NotEqual(t, "INFO", last.Level,
				"the last entry must be the outcome, not the start")
		})
	}
}

// A failure has to carry its reason into the panel. This is AC3: the operator
// reading VIEW LOGS after a failed job should not have to open the container
// log to learn why it failed.
func TestRunMonolithicJobRecordsTheFailureReason(t *testing.T) {
	w := setupWorkerTestDB(t)

	job := database.Job{
		Type:        "scan",
		State:       "running",
		ScopeID:     "not-a-uuid",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	jc := &jobContext{job: job, ctx: context.Background(), processing: true}
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = jc
	w.jobMutex.Unlock()

	w.runMonolithicJob(jc)

	var logs []database.JobLog
	require.NoError(t, w.db.Where("job_id = ?", job.ID).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)

	terminal := logs[len(logs)-1]
	assert.Equal(t, "ERR", terminal.Level)
	assert.Contains(t, terminal.Message, "Job failed")
	assert.Contains(t, terminal.Message, "invalid library UUID",
		"the reason the job failed belongs in the panel")

	// The same reason must be on the job row itself, so the two agree.
	var reloaded database.Job
	require.NoError(t, w.db.First(&reloaded, job.ID).Error)
	assert.Equal(t, "failed", reloaded.State)
	assert.Contains(t, reloaded.ErrorDetail, "invalid library UUID")
}

// A job the operator stopped did not fail. Cancelling one mid-flight makes
// the service call return an error, and a terminal entry that reported that
// error would tell the operator the job broke when they are the reason it
// stopped. finishJob already resolves this by probing the row, and the log
// entry has to agree with the state the job actually ended in.
func TestRunMonolithicJobLogsCancellationRatherThanFailure(t *testing.T) {
	w := setupWorkerTestDB(t)

	job := database.Job{
		Type:        "scan",
		State:       "running",
		ScopeID:     "not-a-uuid",
		RequestedAt: time.Now(),
	}
	require.NoError(t, w.db.Create(&job).Error)

	jc := &jobContext{job: job, ctx: context.Background(), processing: true}
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = jc
	w.jobMutex.Unlock()

	// The operator stops the job while the dispatcher is inside its service
	// call. The scope does not parse, so the call fails - and the row already
	// says cancelled, which is the intent that has to win.
	require.NoError(t, w.db.Model(&database.Job{}).
		Where("id = ?", job.ID).
		Update("state", "cancelled").Error)

	w.runMonolithicJob(jc)

	var logs []database.JobLog
	require.NoError(t, w.db.Where("job_id = ?", job.ID).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)

	terminal := logs[len(logs)-1]
	assert.Equal(t, "WARN", terminal.Level)
	assert.Contains(t, terminal.Message, "cancelled")
	assert.NotContains(t, terminal.Message, "Job failed",
		"a job the operator stopped did not fail")

	var reloaded database.Job
	require.NoError(t, w.db.First(&reloaded, job.ID).Error)
	assert.Equal(t, "cancelled", reloaded.State, "the log and the state must agree")
}

// The ticket's guard: a new case in the switch must not be able to land
// silently. Every exit has to route through completeMonolithicJob, which is
// what writes the terminal entry - so a case that returns on its own, or
// finalises the job itself, is exactly the regression this catches. Same
// reasoning as the stylesheet-coverage guard in the templates package.
func TestEveryDispatchedJobTypeExitsThroughTheLoggingWrapper(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, 0)
	require.NoError(t, err)

	var dispatch *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "runMonolithicJob" {
			dispatch = fn
			break
		}
	}
	require.NotNil(t, dispatch, "runMonolithicJob not found")

	var types []string
	callsWrapper := 0
	ast.Inspect(dispatch.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CaseClause:
			for _, expr := range node.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if value, err := strconv.Unquote(lit.Value); err == nil {
					types = append(types, value)
				}
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "finishJob":
				t.Errorf("runMonolithicJob calls w.finishJob directly; "+
					"a job type dispatched here must finish through "+
					"completeMonolithicJob or VIEW LOGS ends with no outcome")
			case "completeMonolithicJob":
				callsWrapper++
			}
		}
		return true
	})

	assert.NotEmpty(t, types, "the switch should still dispatch job types")
	assert.GreaterOrEqual(t, callsWrapper, len(types),
		"every case needs at least one exit through the logging wrapper")

	// A typo in a case label silently produces a job type the worker will
	// reject at dispatch time, which is the same invisible failure in a new
	// place.
	known := map[string]bool{
		"acquisition": true, "scan": true, "sync": true, "enrich": true,
		"artist_scan": true, "prune": true, "release_monitor": true,
		"index_refresh": true,
	}
	for _, jobType := range types {
		assert.True(t, known[jobType],
			"%q is not a job type this app creates; check the case label", jobType)
	}
}
