package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestEngine creates a Pongo2Engine from a map of template files in a temp dir.
func newTestEngine(t *testing.T, files map[string]string) *Pongo2Engine {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		require.NoError(t, err)
		err = os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}
	engine := NewPongo2(dir, ".html")
	err := engine.LoadFromDir()
	require.NoError(t, err)
	return engine
}

// ---------------------------------------------------------------------------
// Constructor & Load tests
// ---------------------------------------------------------------------------

func TestNewPongo2_CreatesEngine(t *testing.T) {
	engine := NewPongo2("/templates", ".html")
	require.NotNil(t, engine)
	assert.Equal(t, "/templates", engine.directory)
	assert.Equal(t, ".html", engine.extension)
	assert.NotNil(t, engine.pool.New)
}

func TestLoadFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	engine := NewPongo2(dir, ".html")
	err := engine.LoadFromDir()
	assert.NoError(t, err)
}

func TestLoadFromDir_MultipleTemplates(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"index.html":        `<!DOCTYPE html><html>{{ content }}</html>`,
		"partials/header.html": `<header>Welcome</header>`,
	})
	require.NotNil(t, engine)
}

func TestLoadFromDir_WithSubdirectory(t *testing.T) {
	// Test that LoadFromDir correctly handles subdirectory templates
	engine := newTestEngine(t, map[string]string{
		"partials/header.html": `<header>Welcome</header>`,
		"partials/footer.html": `<footer>Goodbye</footer>`,
	})
	require.NotNil(t, engine)
}

// ---------------------------------------------------------------------------
// Render tests
// ---------------------------------------------------------------------------

func TestRender_TemplateFound(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"hello.html": `Hello {{ name }}`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "hello.html", map[string]interface{}{"name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello World", buf.String())
}

func TestRender_TemplateNotFound(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"exists.html": `I exist`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "does_not_exist.html", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template not found")
}

func TestRender_WithoutExtension(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"hello.html": `Hello {{ name }}`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "hello", map[string]interface{}{"name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello World", buf.String())
}

func TestRender_NilBind(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"nilbind.html": `Hello World`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "nilbind.html", nil)
	require.NoError(t, err)
	assert.Equal(t, "Hello World", buf.String())
}

func TestRender_FiberMapBind(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"fiber.html": `value: {{ key }}`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "fiber.html", fiber.Map{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, "value: val", buf.String())
}

func TestRender_Pongo2ContextBind(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"pongo.html": `ctx: {{ key }}`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "pongo.html", pongo2.Context{"key": "ctx"})
	require.NoError(t, err)
	assert.Equal(t, "ctx: ctx", buf.String())
}

func TestRender_InvalidBindType(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"invalid.html": `Hello World`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "invalid.html", 42)
	// Graceful degradation: renders with empty context, no error
	require.NoError(t, err)
	assert.Equal(t, "Hello World", buf.String())
}

func TestRender_UpperFilter(t *testing.T) {
	engine := newTestEngine(t, map[string]string{
		"upper.html": `{{ "hello"|upper }}`,
	})
	var buf bytes.Buffer
	err := engine.Render(&buf, "upper.html", nil)
	require.NoError(t, err)
	assert.Equal(t, "HELLO", buf.String())
}

// ---------------------------------------------------------------------------
// tryConvertToMap tests
// ---------------------------------------------------------------------------

func TestTryConvertToMap_FiberMap(t *testing.T) {
	fm := fiber.Map{"key": "value", "num": 123}
	result, ok := tryConvertToMap(fm)
	assert.True(t, ok)
	assert.Equal(t, "value", result["key"])
	assert.Equal(t, 123, result["num"])
}

func TestTryConvertToMap_Nil(t *testing.T) {
	result, ok := tryConvertToMap(nil)
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestTryConvertToMap_NonMap(t *testing.T) {
	result, ok := tryConvertToMap(42)
	assert.False(t, ok)
	assert.Nil(t, result)
}
