package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// formatFileSize tests
// ---------------------------------------------------------------------------

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes under unit", 512, "512 B"},
		{"exact KB", 1024, "1.0 KB"},
		{"KB with fraction", 1536, "1.5 KB"},
		{"exact MB", 1048576, "1.0 MB"},
		{"MB with fraction", 1572864, "1.5 MB"},
		{"exact GB", 1073741824, "1.0 GB"},
		{"GB with fraction", 1610612736, "1.5 GB"},
		{"exact TB", 1099511627776, "1.0 TB"},
		{"TB with fraction", 1649267441664, "1.5 TB"},
		// 1024^5 = 1125899906842624 yields "1.0 PB" (PB unit is supported)
		{"PB-scale", 1125899906842624, "1.0 PB"},
		// Negative values: since bytes < 1024 is true for negatives, they format as raw bytes
		{"negative", -1024, "-1024 B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFileSize(tt.bytes)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Command structure tests
// ---------------------------------------------------------------------------

func getSubcommandNames(cmd *cobra.Command) []string {
	names := make([]string, len(cmd.Commands()))
	for i, c := range cmd.Commands() {
		names[i] = c.Name()
	}
	return names
}

func getSubcommandUseStrings(cmd *cobra.Command) []string {
	uses := make([]string, len(cmd.Commands()))
	for i, c := range cmd.Commands() {
		uses[i] = c.Use
	}
	return uses
}

// helper to validate a subcommand has required fields set
func assertSubcommand(t *testing.T, sub *cobra.Command, use string) {
	assert.NotEmpty(t, sub.Use, "subcommand %q should have Use set", use)
	assert.NotEmpty(t, sub.Short, "subcommand %q should have Short set", use)
	assert.NotNil(t, sub.Run, "subcommand %q should have Run function", use)
}

func TestStatusCmd(t *testing.T) {
	cmd := statusCmd()

	assert.Equal(t, "status", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.Run)
}

func TestConfigCmd(t *testing.T) {
	cmd := configCmd()

	assert.Equal(t, "config", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subcommands := cmd.Commands()
	assert.NotEmpty(t, subcommands, "configCmd should have subcommands")

	subcommandNames := getSubcommandNames(cmd)
	assert.Contains(t, subcommandNames, "list", "configCmd should have 'list' subcommand")

	// Verify the list subcommand structure
	for _, sub := range subcommands {
		assertSubcommand(t, sub, sub.Use)
	}
}

func TestWatchlistCmd(t *testing.T) {
	cmd := watchlistCmd()

	assert.Equal(t, "watchlist", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subcommandNames := getSubcommandNames(cmd)
	expectedSubcommands := []string{"list", "add", "sync", "import"}
	for _, expected := range expectedSubcommands {
		assert.Contains(t, subcommandNames, expected, "watchlistCmd should have '%s' subcommand", expected)
	}

	// Verify all subcommands are properly structured
	for _, sub := range cmd.Commands() {
		assertSubcommand(t, sub, sub.Use)
	}
}

func TestLibraryCmd(t *testing.T) {
	cmd := libraryCmd()

	assert.Equal(t, "library", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subcommandNames := getSubcommandNames(cmd)
	expectedSubcommands := []string{"list", "add", "scan", "prune", "rm", "duplicates"}
	for _, expected := range expectedSubcommands {
		assert.Contains(t, subcommandNames, expected, "libraryCmd should have '%s' subcommand", expected)
	}

	// Verify all subcommands are properly structured
	for _, sub := range cmd.Commands() {
		assertSubcommand(t, sub, sub.Use)
	}
}

func TestProfileCmd(t *testing.T) {
	cmd := profileCmd()

	assert.Equal(t, "profile", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subcommandNames := getSubcommandNames(cmd)
	expectedSubcommands := []string{"list", "add", "rm", "set-default"}
	for _, expected := range expectedSubcommands {
		assert.Contains(t, subcommandNames, expected, "profileCmd should have '%s' subcommand", expected)
	}

	// Verify all subcommands are properly structured
	for _, sub := range cmd.Commands() {
		assertSubcommand(t, sub, sub.Use)
	}
}

func TestStatsCmd(t *testing.T) {
	cmd := statsCmd()

	assert.Equal(t, "stats", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subcommandNames := getSubcommandNames(cmd)
	expectedSubcommands := []string{"summary", "jobs", "library"}
	for _, expected := range expectedSubcommands {
		assert.Contains(t, subcommandNames, expected, "statsCmd should have '%s' subcommand", expected)
	}

	// Verify all subcommands are properly structured
	for _, sub := range cmd.Commands() {
		assertSubcommand(t, sub, sub.Use)
	}
}

// ---------------------------------------------------------------------------
// printJSON tests
// ---------------------------------------------------------------------------

func TestPrintJSON(t *testing.T) {
	// Save and restore stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	testData := map[string]string{"key": "value", "name": "test"}
	printJSON(testData)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()
	assert.Contains(t, output, `"key"`, "JSON output should contain key")
	assert.Contains(t, output, `"value"`, "JSON output should contain value")
	assert.Contains(t, output, `"name"`, "JSON output should contain name")
	assert.Contains(t, output, `"test"`, "JSON output should contain test")
}

func TestPrintJSONArray(t *testing.T) {
	// Save and restore stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	testData := []string{"item1", "item2", "item3"}
	printJSON(testData)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()
	assert.Contains(t, output, `item1`, "JSON output should contain item1")
	assert.Contains(t, output, `item2`, "JSON output should contain item2")
	assert.Contains(t, output, `item3`, "JSON output should contain item3")
}

func TestPrintJSONStruct(t *testing.T) {
	// Save and restore stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	testData := struct {
		Count int    `json:"count"`
		Label string `json:"label"`
	}{Count: 42, Label: "answer"}

	printJSON(testData)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()
	assert.Contains(t, output, `"count"`, "JSON output should contain count field")
	assert.Contains(t, output, `42`, "JSON output should contain count value")
	assert.Contains(t, output, `"label"`, "JSON output should contain label field")
	assert.Contains(t, output, `answer`, "JSON output should contain label value")
}

// ---------------------------------------------------------------------------
// handleError tests
// ---------------------------------------------------------------------------

// Note: handleError is not directly testable because it calls os.Exit(1),
// which terminates the process. The function outputs "Error: %v\n" to stderr
// and exits with code 1 when jsonOutput is false (the default).
// To test this function properly, a separate test binary or testmain would be needed.

// ---------------------------------------------------------------------------
// handleError tests
// ---------------------------------------------------------------------------

func TestHandleError(t *testing.T) {
	// Save and restore osExit
	oldExit := osExit
	var exitCode int
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = oldExit }()

	tests := []struct {
		name       string
		err        error
		wantExit   bool
		wantCode   int
	}{
		{
			name:     "nil error should not exit",
			err:      nil,
			wantExit: false,
			wantCode: 0,
		},
		{
			name:     "non-nil error should exit with code 1",
			err:      errors.New("test error"),
			wantExit: true,
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exitCode = 0 // reset
			handleError(tt.err)
			if tt.wantExit {
				assert.Equal(t, tt.wantCode, exitCode, "exit code should match")
			} else {
				assert.Equal(t, 0, exitCode, "should not have called exit")
			}
		})
	}
}

func TestHandleError_Output(t *testing.T) {
	// Save and restore osExit
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { osExit = oldExit }()

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	handleError(errors.New("test error message"))

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	assert.Contains(t, output, "Error: test error message\n")
}

func TestHandleError_JsonOutput(t *testing.T) {
	// Save and restore osExit
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { osExit = oldExit }()

	// Save and restore jsonOutput flag
	oldJsonOutput := jsonOutput
	jsonOutput = true
	defer func() { jsonOutput = oldJsonOutput }()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	handleError(errors.New("json error"))

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, `"json error"`)
}

// ---------------------------------------------------------------------------
// Command Run tests
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err, "should connect to in-memory DB")
	return db
}

func TestStatusCmd_Run(t *testing.T) {
	testDB := setupTestDB(t)

	// Save and restore package vars
	oldDB := db
	oldCfg := cfg
	defer func() { db = oldDB; cfg = oldCfg }()

	db = testDB
	cfg = &config.Config{
		DatabaseURL: ":memory:",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := statusCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, err, "statusCmd should not return error")

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	assert.Contains(t, output, "Database:")
	assert.Contains(t, output, "Gonic:")
	assert.Contains(t, output, "Slskd:")
}

func TestConfigCmd_List_Run(t *testing.T) {
	testDB := setupTestDB(t)

	// Save and restore package vars
	oldDB := db
	oldCfg := cfg
	defer func() { db = oldDB; cfg = oldCfg }()

	db = testDB
	cfg = &config.Config{
		DatabaseURL: ":memory:",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := configCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, err, "config list should not return error")

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	// Should output settings map (may be empty)
	assert.NotEmpty(t, output, "should produce some output")
}

func TestProfileCmd_List_Run(t *testing.T) {
	testDB := setupTestDB(t)

	// Save and restore package vars
	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{
		DatabaseURL: ":memory:",
	}

	cmd := profileCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()

	// handleError is called when table doesn't exist - we just verify no panic
	require.NoError(t, err, "should not panic even if table missing")
}

func TestStatsCmd_Summary_Run(t *testing.T) {
	testDB := setupTestDB(t)

	// Save and restore package vars
	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{
		DatabaseURL: ":memory:",
	}

	cmd := statsCmd()
	cmd.SetArgs([]string{"summary"})
	err := cmd.Execute()

	// handleError is called when table doesn't exist - we just verify no panic
	require.NoError(t, err, "should not panic even if table missing")
}

// ---------------------------------------------------------------------------
// Additional Command Execution Tests
// ---------------------------------------------------------------------------

func setupTestDBWithMigrate(t *testing.T) *gorm.DB {
	testDB := setupTestDB(t)
	err := database.Migrate(testDB)
	require.NoError(t, err, "should migrate database")
	return testDB
}

func captureStdout(t *testing.T) (func() string, func()) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	return func() string {
		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		os.Stdout = oldStdout
		return buf.String()
	}, func() {
		os.Stdout = oldStdout
	}
}

func captureStderr(t *testing.T) (func() string, func()) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	return func() string {
		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		os.Stderr = oldStderr
		return buf.String()
	}, func() {
		os.Stderr = oldStderr
	}
}

func TestWatchlistCmd_List_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a quality profile first (required for watchlist)
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless: true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	// Create a watchlist
	wl := database.Watchlist{
		ID:               uuid.New(),
		Name:             "Test Watchlist",
		SourceType:       "spotify",
		SourceURI:        "spotify:playlist:123",
		QualityProfileID: profile.ID,
	}
	err = db.Create(&wl).Error
	require.NoError(t, err, "should create watchlist")

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist list should not error")
	assert.Contains(t, output, "Test Watchlist")
	assert.Contains(t, output, "spotify")
}

func TestWatchlistCmd_List_Run_Empty(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// No data created - list should be empty
	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist list should not error on empty DB")
	// Empty DB produces no output (the CLI doesn't print "No watchlists")
	assert.NotContains(t, output, "Test Watchlist", "empty DB should not have watchlists")
}

func TestWatchlistCmd_Add_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a quality profile first
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"add", "New Watchlist", "rss_feed", "https://example.com/feed.xml"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist add should not error")
	assert.Contains(t, output, "Successfully added watchlist")
	assert.Contains(t, output, "New Watchlist")
}

func TestWatchlistCmd_Add_Run_InvalidArgs(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	cmd := watchlistCmd()
	// Missing required args - should fail
	cmd.SetArgs([]string{"add", "OnlyTwoArgs"})
	err := cmd.Execute()
	// Cobra should return error for missing args
	assert.Error(t, err, "should error on missing args")
}

func TestWatchlistCmd_Sync_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a profile and watchlist
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	wl := database.Watchlist{
		ID:               uuid.New(),
		Name:             "Test Watchlist",
		SourceType:       "spotify",
		SourceURI:        "spotify:playlist:123",
		QualityProfileID: profile.ID,
	}
	err = db.Create(&wl).Error
	require.NoError(t, err, "should create watchlist")

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"sync", wl.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist sync should not error")
	assert.Contains(t, output, "Synchronization job")
}

func TestWatchlistCmd_Sync_Run_InvalidUUID(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"sync", "not-a-valid-uuid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestWatchlistCmd_Sync_Run_NotFound(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Sync with a valid UUID but non-existent watchlist
	// Note: SyncWatchlist doesn't validate watchlist exists, it just creates a job
	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"sync", uuid.New().String()})
	err := cmd.Execute()
	output := getOutput()

	// The command succeeds and creates a job (even though watchlist doesn't exist)
	require.NoError(t, err, "sync command should not error")
	assert.Contains(t, output, "Synchronization job")
}

func TestWatchlistCmd_Import_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	oldStdin := os.Stdin
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit; os.Stdin = oldStdin }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a quality profile first
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	// Create stdin with JSON input
	input := fmt.Sprintf(`[{"name": "Imported Watchlist", "source_type": "rss_feed", "source_uri": "https://example.com/feed.xml", "quality_profile_id": "%s"}]`, profile.ID.String())
	r, w, _ := os.Pipe()
	os.Stdin = r
	_, err = w.WriteString(input)
	require.NoError(t, err)
	w.Close()

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"import"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist import should not error")
	assert.Contains(t, output, "Successfully imported")
}

func TestLibraryCmd_List_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library list should not error")
	assert.Contains(t, output, "Test Library")
	assert.Contains(t, output, "/music/test")
}

func TestLibraryCmd_List_Run_Empty(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// No data created - list should be empty
	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library list should not error on empty DB")
	assert.NotContains(t, output, "Test Library", "empty DB should not have libraries")
}

func TestLibraryCmd_Add_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"add", "New Library", "/music/new"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library add should not error")
	assert.Contains(t, output, "Successfully added library")
	assert.Contains(t, output, "New Library")
}

func TestLibraryCmd_Scan_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"scan", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library scan should not error")
	assert.Contains(t, output, "Successfully queued scan job")
}

func TestLibraryCmd_Prune_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"prune", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library prune should not error")
	assert.Contains(t, output, "Successfully queued prune job")
}

func TestLibraryCmd_Rm_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"rm", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library rm should not error")
	assert.Contains(t, output, "Successfully deleted library")

	// Verify library is gone
	var count int64
	db.Model(&database.Library{}).Count(&count)
	assert.Equal(t, int64(0), count, "library should be deleted")
}

func TestLibraryCmd_Duplicates_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// No data created - duplicates should show no results
	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"duplicates"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library duplicates should not error")
	assert.NotContains(t, output, "Test Library", "empty DB should not have libraries")
}

func TestProfileCmd_List_Run_WithData(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a profile
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC,WAV",
		MinBitrate:      320,
		Description:     "Test description",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile list should not error")
	assert.Contains(t, output, "Test Profile")
	assert.Contains(t, output, "[DEFAULT]")
	assert.Contains(t, output, "FLAC,WAV")
}

func TestProfileCmd_List_Run_Empty(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// No data created - list should be empty
	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile list should not error on empty DB")
	assert.NotContains(t, output, "Test Profile", "empty DB should not have profiles")
}

func TestProfileCmd_Add_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"add", "New Profile"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile add should not error")
	assert.Contains(t, output, "Successfully created profile")
	assert.Contains(t, output, "New Profile")
}

func TestProfileCmd_Add_Run_WithFlags(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"add", "Flagged Profile",
		"--description", "A profile with flags",
		"--lossless",
		"--formats", "FLAC,ALAC",
		"--min-bitrate", "256",
		"--prefer-bitrate", "320",
		"--scene",
		"--web"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile add with flags should not error")
	assert.Contains(t, output, "Successfully created profile")
	assert.Contains(t, output, "Flagged Profile")
}

func TestProfileCmd_Rm_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a profile
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "To Delete",
		IsDefault:       false,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"rm", profile.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile rm should not error")
	assert.Contains(t, output, "Successfully deleted profile")

	// Verify profile is gone
	var count int64
	db.Model(&database.QualityProfile{}).Count(&count)
	assert.Equal(t, int64(0), count, "profile should be deleted")
}

func TestProfileCmd_SetDefault_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create two profiles
	profile1 := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Profile One",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	profile2 := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Profile Two",
		IsDefault:       false,
		PreferLossless:  false,
		AllowedFormats:  "MP3",
	}
	err := db.Create(&profile1).Error
	require.NoError(t, err, "should create profile1")
	err = db.Create(&profile2).Error
	require.NoError(t, err, "should create profile2")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"set-default", profile2.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile set-default should not error")
	assert.Contains(t, output, "Successfully set default profile")

	// Verify profile2 is now default
	var updated database.QualityProfile
	db.First(&updated, "id = ?", profile2.ID)
	assert.True(t, updated.IsDefault, "profile2 should now be default")

	// profile1 should no longer be default
	var updated1 database.QualityProfile
	db.First(&updated1, "id = ?", profile1.ID)
	assert.False(t, updated1.IsDefault, "profile1 should no longer be default")
}

func TestProfileCmd_SetDefault_Run_InvalidUUID(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"set-default", "not-a-valid-uuid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestStatsCmd_Jobs_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"jobs"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats jobs should not error")
	assert.Contains(t, output, "Jobs")
}

func TestStatsCmd_Library_Run(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"library"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats library should not error")
	assert.Contains(t, output, "Library")
}

// ---------------------------------------------------------------------------
// Flag Parsing Tests
// ---------------------------------------------------------------------------

func TestProfileCmd_Add_FlagParsing(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Test that all flags are properly parsed - pass just the name as positional arg
	cmd := profileCmd()
	cmd.SetArgs([]string{
		"add", "FlagTest",
		"--description", "desc",
		"--lossless",
		"--formats", "FLAC",
		"--min-bitrate", "128",
		"--prefer-bitrate", "320",
		"--scene",
		"--web",
	})
	err := cmd.Execute()

	require.NoError(t, err, "profile add should parse all flags")
}

// ---------------------------------------------------------------------------
// JSON Output Tests
// ---------------------------------------------------------------------------

func TestWatchlistCmd_List_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a quality profile and watchlist
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	wl := database.Watchlist{
		ID:               uuid.New(),
		Name:             "JSON Watchlist",
		SourceType:       "spotify",
		SourceURI:        "spotify:playlist:123",
		QualityProfileID: profile.ID,
	}
	err = db.Create(&wl).Error
	require.NoError(t, err, "should create watchlist")

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist list should not error with jsonOutput")
	assert.Contains(t, output, "JSON Watchlist")
	assert.Contains(t, output, "spotify")
}

func TestStatsCmd_Summary_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"summary"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats summary should not error with jsonOutput")
	assert.Contains(t, output, `"jobs"`)
	assert.Contains(t, output, `"library"`)
}

// ---------------------------------------------------------------------------
// Error Handling Tests
// ---------------------------------------------------------------------------

func TestWatchlistCmd_Sync_Run_InvalidUUIDHandling(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"sync", "invalid-uuid-format"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestLibraryCmd_Scan_Run_InvalidUUIDHandling(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"scan", "not-valid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestLibraryCmd_Prune_Run_InvalidUUIDHandling(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"prune", "bad-uuid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestLibraryCmd_Rm_Run_InvalidUUIDHandling(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"rm", "invalid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

func TestProfileCmd_Rm_Run_InvalidUUIDHandling(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	getStderr, _ := captureStderr(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"rm", "not-valid"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// handleError writes to stderr but doesn't return an error
	assert.Contains(t, stderrOutput, "invalid UUID")
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

func TestWatchlistCmd_Add_Run_NoDefaultProfile(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// No quality profile created - should fail with "no default quality profile found"
	getStderr, _ := captureStderr(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"add", "test", "spotify", "spotify:playlist:abc"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	assert.Contains(t, stderrOutput, "no default quality profile found")
}

func TestWatchlistCmd_Add_Run_UnsupportedSourceType(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a quality profile
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	// Use "spotify" which is not a registered provider (should be spotify_playlist, etc.)
	getStderr, _ := captureStderr(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"add", "test", "spotify", "spotify:playlist:abc"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	assert.Contains(t, stderrOutput, "unsupported source type: spotify")
}

func TestWatchlistCmd_Import_Run_MalformedJSON(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	oldStdin := os.Stdin
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit; os.Stdin = oldStdin }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create stdin with invalid JSON
	r, w, _ := os.Pipe()
	os.Stdin = r
	_, err := w.WriteString("not valid json {")
	require.NoError(t, err)
	w.Close()

	getStderr, _ := captureStderr(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"import"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	assert.Contains(t, stderrOutput, "failed to parse JSON")
}

func TestWatchlistCmd_Import_Run_PartialFailure(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	oldStdin := os.Stdin
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit; os.Stdin = oldStdin }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a quality profile
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	// JSON array with one valid entry and one invalid (unsupported source type)
	input := fmt.Sprintf(`[{"name": "Valid Watchlist", "source_type": "rss_feed", "source_uri": "https://example.com/feed.xml", "quality_profile_id": "%s"}, {"name": "Invalid Watchlist", "source_type": "unsupported_type", "source_uri": "test", "quality_profile_id": "%s"}]`, profile.ID.String(), profile.ID.String())
	r, w, _ := os.Pipe()
	os.Stdin = r
	_, err = w.WriteString(input)
	require.NoError(t, err)
	w.Close()

	getStderr, _ := captureStderr(t)
	getStdout, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"import"})
	_ = cmd.Execute()
	stderrOutput := getStderr()
	stdoutOutput := getStdout()

	// Should log error for the invalid entry
	assert.Contains(t, stderrOutput, "unsupported source type: unsupported_type")
	// Should report 1 successfully imported
	assert.Contains(t, stdoutOutput, "Successfully imported 1 watchlists")
}

func TestLibraryCmd_Add_Run_DuplicatePath(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library first
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library",
		Path: "/music",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	// Try to add another library with the same path
	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"add", "Duplicate Library", "/music"})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	// Should fail with duplicate path error
	assert.Contains(t, stderrOutput, "UNIQUE constraint failed")
}

func TestLibraryCmd_Scan_Run_NotFound(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Use a non-existent library UUID
	nonExistentID := uuid.New()
	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"scan", nonExistentID.String()})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	assert.Contains(t, stderrOutput, "record not found")
}

func TestLibraryCmd_Prune_Run_NotFound(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Use a non-existent library UUID
	nonExistentID := uuid.New()
	getStderr, _ := captureStderr(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"prune", nonExistentID.String()})
	_ = cmd.Execute()
	stderrOutput := getStderr()

	assert.Contains(t, stderrOutput, "record not found")
}

func TestLibraryCmd_Rm_Run_NotFound(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Use a non-existent library UUID - DeleteLibrary returns no error when not found
	nonExistentID := uuid.New()
	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"rm", nonExistentID.String()})
	_ = cmd.Execute()
	output := getOutput()

	// DeleteLibrary succeeds silently even if library not found (GORM Delete returns nil)
	assert.Contains(t, output, "Successfully deleted library")
	// But verify no library was actually deleted (DB is still empty)
	var count int64
	db.Model(&database.Library{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestProfileCmd_Rm_Run_NotFound(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Use a non-existent profile UUID - DeleteProfile returns no error when not found
	nonExistentID := uuid.New()
	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"rm", nonExistentID.String()})
	_ = cmd.Execute()
	output := getOutput()

	// DeleteProfile succeeds silently even if profile not found (GORM Delete returns nil)
	assert.Contains(t, output, "Successfully deleted profile")
	// But verify no profile was actually deleted (DB is still empty)
	var count int64
	db.Model(&database.QualityProfile{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

// ---------------------------------------------------------------------------
// JSON Output Tests - Status, Config, Stats, Library
// ---------------------------------------------------------------------------

func TestStatusCmd_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := statusCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "statusCmd should not error with jsonOutput")
	// SystemStatus fields are: database_connected, slskd_connected, gonic_connected, message
	assert.Contains(t, output, `"database_connected"`)
	assert.Contains(t, output, `"slskd_connected"`)
	assert.Contains(t, output, `"gonic_connected"`)
}

func TestConfigCmd_List_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := configCmd()
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "config list should not error with jsonOutput")
	// ReadConfig returns map[string]string which serializes as a JSON object
	assert.Contains(t, output, "{")
	assert.Contains(t, output, "}")
}

func TestStatsCmd_Jobs_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"jobs"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats jobs should not error with jsonOutput")
	// JobStats has fields: total, queued, running, succeeded, failed, success_rate
	assert.Contains(t, output, `"total"`)
	assert.Contains(t, output, `"queued"`)
}

func TestStatsCmd_Library_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"library"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats library should not error with jsonOutput")
	// LibraryStats has fields: total_tracks, total_size, total_size_mb, format_breakdown
	assert.Contains(t, output, `"total_tracks"`)
	assert.Contains(t, output, `"total_size_mb"`)
}

func TestLibraryCmd_Add_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"add", "Test JSON", "/music/json"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library add should not error with jsonOutput")
	// Library has json fields: id, name, path
	assert.Contains(t, output, `"name"`)
	assert.Contains(t, output, `"path"`)
}

func TestLibraryCmd_Scan_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	// Create a library first
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library for Scan",
		Path: "/music/testscan",
	}
	err := testDB.Create(&library).Error
	require.NoError(t, err, "should create library")

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"scan", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library scan should not error with jsonOutput")
	// Job struct has no JSON tags, so fields serialize as PascalCase
	assert.Contains(t, output, `"ID"`)
	assert.Contains(t, output, `"Type"`)
	assert.Contains(t, output, `"State"`)
}

func TestLibraryCmd_Prune_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	// Create a library first
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library for Prune",
		Path: "/music/testprune",
	}
	err := testDB.Create(&library).Error
	require.NoError(t, err, "should create library")

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"prune", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library prune should not error with jsonOutput")
	// Job struct has no JSON tags, so fields serialize as PascalCase
	assert.Contains(t, output, `"ID"`)
	assert.Contains(t, output, `"Type"`)
	assert.Contains(t, output, `"State"`)
}

func TestLibraryCmd_Rm_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	// Create a library first
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Library for Rm",
		Path: "/music/testrm",
	}
	err := testDB.Create(&library).Error
	require.NoError(t, err, "should create library")

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"rm", library.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library rm should not error with jsonOutput")
	// DeleteLibrary returns map[string]string{"status": "deleted"}
	assert.Contains(t, output, `"status"`)
	assert.Contains(t, output, `"deleted"`)
}

func TestLibraryCmd_Duplicates_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"duplicates"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library duplicates should not error with jsonOutput")
	// ListDuplicates returns []DuplicateGroup which serializes as JSON array,
	// or null when no duplicates found
	assert.True(t, strings.Contains(output, "[") || strings.Contains(output, "null"),
		"output should be valid JSON (array or null), got: %s", output)
}

func TestLibraryCmd_List_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a library
	library := database.Library{
		ID:   uuid.New(),
		Name: "JSON Library",
		Path: "/music/json-test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := libraryCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "library list should not error with jsonOutput")
	assert.Contains(t, output, "JSON Library")
	assert.Contains(t, output, `/music/json-test`)
}

func TestWatchlistCmd_Add_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a quality profile first
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "Test Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := watchlistCmd()
	cmd.SetArgs([]string{"add", "JSON Watchlist", "rss_feed", "https://example.com/json-feed.xml"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "watchlist add should not error with jsonOutput")
	assert.Contains(t, output, "JSON Watchlist")
}

func TestProfileCmd_List_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a profile
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "JSON Profile",
		IsDefault:       true,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"list"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile list should not error with jsonOutput")
	assert.Contains(t, output, "JSON Profile")
}

func TestProfileCmd_Add_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"add", "JSON Profile"})
	err := cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile add should not error with jsonOutput")
	assert.Contains(t, output, "JSON Profile")
}

func TestProfileCmd_Rm_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a profile to delete
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "To Delete JSON",
		IsDefault:       false,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"rm", profile.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile rm should not error with jsonOutput")
	assert.Contains(t, output, `"status"`)
}

func TestStatsCmd_Summary_Run_WithData(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library so activity data is non-zero
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Lib",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"summary"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats summary should not error with data")
	assert.Contains(t, output, "Jobs (24h):")
	assert.Contains(t, output, "Library:")
	assert.Contains(t, output, "Activity:")
	assert.Contains(t, output, "Libraries: 1")
}

func TestStatsCmd_Library_Run_WithData(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}

	// Create a library and a track to get format breakdown
	library := database.Library{
		ID:   uuid.New(),
		Name: "Test Lib",
		Path: "/music/test",
	}
	err := db.Create(&library).Error
	require.NoError(t, err, "should create library")

	track := database.Track{
		ID:        uuid.New(),
		LibraryID: library.ID,
		Title:     "Test Track",
		Artist:    "Test Artist",
		Path:      "/music/test/test.flac",
		Format:    "FLAC",
		FileSize:  1024 * 1024,
	}
	err = db.Create(&track).Error
	require.NoError(t, err, "should create track")

	getOutput, _ := captureStdout(t)
	cmd := statsCmd()
	cmd.SetArgs([]string{"library"})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "stats library should not error with data")
	assert.Contains(t, output, "Library Statistics:")
	assert.Contains(t, output, "Total Tracks: 1")
	assert.Contains(t, output, "Format Breakdown:")
	assert.Contains(t, output, "FLAC")
}

func TestProfileCmd_SetDefault_Run_JSONOutput(t *testing.T) {
	testDB := setupTestDBWithMigrate(t)

	oldDB := db
	oldCfg := cfg
	oldJsonOutput := jsonOutput
	oldExit := osExit
	osExit = func(code int) {}
	defer func() { db = oldDB; cfg = oldCfg; jsonOutput = oldJsonOutput; osExit = oldExit }()

	db = testDB
	cfg = &config.Config{DatabaseURL: ":memory:"}
	jsonOutput = true

	// Create a profile to set as default
	profile := database.QualityProfile{
		ID:              uuid.New(),
		Name:            "New Default",
		IsDefault:       false,
		PreferLossless:  true,
		AllowedFormats:  "FLAC",
	}
	err := db.Create(&profile).Error
	require.NoError(t, err, "should create profile")

	getOutput, _ := captureStdout(t)
	cmd := profileCmd()
	cmd.SetArgs([]string{"set-default", profile.ID.String()})
	err = cmd.Execute()
	output := getOutput()

	require.NoError(t, err, "profile set-default should not error with jsonOutput")
	assert.Contains(t, output, `"status"`)
}