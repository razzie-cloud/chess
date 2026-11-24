package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"strconv"
)

//go:embed templates.html static
var assets embed.FS
var templates = template.Must(template.New("chess").Funcs(template.FuncMap{
	"json": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	"inc":  func(n int) int { return n + 1 },
	"ply":  func(n int) string { return strconv.Itoa(n/2 + 1) },
}).ParseFS(assets, "templates.html"))

func Render(w io.Writer, name string, data any) error {
	return templates.ExecuteTemplate(w, name, data)
}
func Static() fs.FS { f, _ := fs.Sub(assets, "static"); return f }
