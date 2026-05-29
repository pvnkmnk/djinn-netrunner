package templates

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/flosch/pongo2/v6"
)

// init registers Jinja2-compatible filters with pongo2.
func init() {
	// strftime: placeholder, handlers pass pre-formatted time strings
	pongo2.RegisterFilter("upper", func(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		if s, ok := in.Interface().(string); ok {
			return pongo2.AsValue(strings.ToUpper(s)), nil
		}
		return in, nil
	})
}

// Pongo2Engine implements fiber.Views using pongo2 (Jinja2-compatible templates).
type Pongo2Engine struct {
	directory string
	extension string
	pool      sync.Pool
}

// NewPongo2 creates a new pongo2 template engine.
func NewPongo2(directory, extension string) *Pongo2Engine {
	engine := &Pongo2Engine{
		directory: directory,
		extension: extension,
	}
	engine.pool.New = func() any {
		return pongo2.NewSet("pongo2", pongo2.MustNewLocalFileSystemLoader(directory))
	}
	return engine
}

// Load implements fiber.Views.
func (e *Pongo2Engine) Load() error {
	return e.LoadFromDir()
}

// Render implements fiber.Views. The layouts parameter is ignored since pongo2
// templates use {% extends %} for layout inheritance.
func (e *Pongo2Engine) Render(w io.Writer, name string, bind interface{}, layouts ...string) error {
	set := e.pool.Get().(*pongo2.TemplateSet)
	defer e.pool.Put(set)

	// pongo2 expects "/" as path separator even on Windows
	tplName := strings.ReplaceAll(name, string(os.PathSeparator), "/")

	tpl, err := set.FromFile(tplName)
	if err != nil {
		tpl, err = set.FromFile(tplName + e.extension)
	}
	if err != nil {
		return fmt.Errorf("pongo2: template not found: %s: %w", name, err)
	}

	var buf bytes.Buffer
	var ctx pongo2.Context
	if bind == nil {
		ctx = pongo2.Context{}
	} else if m, ok := bind.(map[string]interface{}); ok {
		ctx = pongo2.Context(m)
	} else {
		ctx = pongo2.Context{}
	}
	if err := tpl.ExecuteWriter(ctx, &buf); err != nil {
		return fmt.Errorf("pongo2: render error for %s: %w", name, err)
	}

	_, err = w.Write(buf.Bytes())
	return err
}

// LoadFromDir preloads all templates from the directory for faster rendering.
func (e *Pongo2Engine) LoadFromDir() error {
	set := e.pool.Get().(*pongo2.TemplateSet)
	defer e.pool.Put(set)

	pattern := filepath.Join(e.directory, "**", "*"+e.extension)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, match := range matches {
		rel, _ := filepath.Rel(e.directory, match)
		rel = strings.ReplaceAll(rel, string(os.PathSeparator), "/")
		if _, err := set.FromFile(rel); err != nil {
			return fmt.Errorf("preload %s: %w", rel, err)
		}
	}
	return nil
}
