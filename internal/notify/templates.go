package notify

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	htemplate "html/template"
	"io/fs"
	"strings"
	ttemplate "text/template"
)

//go:embed templates/email/*.tmpl
var emailFS embed.FS

// ErrUnknownTemplate : aucun template email de ce nom.
var ErrUnknownTemplate = errors.New("template email inconnu")

const layoutFile = "layout.html.tmpl"

// Templates rend les emails à partir de templates/email/<nom>.{subject,txt,html}.tmpl.
// Sujet et texte : text/template (pas d'échappement HTML) ; HTML : html/template
// habillé par layout.html.tmpl. Les données sont une map (le payload JSON de
// l'outbox) ; une clé absente est une erreur.
type Templates struct {
	subject map[string]*ttemplate.Template
	text    map[string]*ttemplate.Template
	html    map[string]*htemplate.Template
}

// NewTemplates charge et valide tous les templates embarqués.
func NewTemplates() (*Templates, error) {
	return loadTemplates(emailFS, "templates/email")
}

func loadTemplates(fsys fs.FS, dir string) (*Templates, error) {
	t := &Templates{
		subject: map[string]*ttemplate.Template{},
		text:    map[string]*ttemplate.Template{},
		html:    map[string]*htemplate.Template{},
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("templates email: %w", err)
	}
	layout, err := fs.ReadFile(fsys, dir+"/"+layoutFile)
	if err != nil {
		return nil, fmt.Errorf("templates email: %w", err)
	}
	for _, en := range entries {
		file := en.Name()
		if file == layoutFile {
			continue
		}
		body, err := fs.ReadFile(fsys, dir+"/"+file)
		if err != nil {
			return nil, fmt.Errorf("templates email: %w", err)
		}
		switch {
		case strings.HasSuffix(file, ".subject.tmpl"):
			name := strings.TrimSuffix(file, ".subject.tmpl")
			if t.subject[name], err = ttemplate.New(file).Option("missingkey=error").Parse(string(body)); err != nil {
				return nil, fmt.Errorf("templates email %s: %w", file, err)
			}
		case strings.HasSuffix(file, ".txt.tmpl"):
			name := strings.TrimSuffix(file, ".txt.tmpl")
			if t.text[name], err = ttemplate.New(file).Option("missingkey=error").Parse(string(body)); err != nil {
				return nil, fmt.Errorf("templates email %s: %w", file, err)
			}
		case strings.HasSuffix(file, ".html.tmpl"):
			name := strings.TrimSuffix(file, ".html.tmpl")
			ht := htemplate.New(file).Option("missingkey=error")
			if _, err = ht.Parse(string(layout)); err != nil {
				return nil, fmt.Errorf("templates email %s: %w", layoutFile, err)
			}
			if _, err = ht.Parse(string(body)); err != nil {
				return nil, fmt.Errorf("templates email %s: %w", file, err)
			}
			t.html[name] = ht
		}
	}
	for name := range t.subject {
		if t.text[name] == nil {
			return nil, fmt.Errorf("templates email %s: version texte manquante", name)
		}
	}
	return t, nil
}

// Has indique si le template name existe.
func (t *Templates) Has(name string) bool { return t.subject[name] != nil && t.text[name] != nil }

// Render produit Subject, Text et HTML (si le template HTML existe). To est
// laissé vide.
func (t *Templates) Render(name string, data map[string]any) (Email, error) {
	if !t.Has(name) {
		return Email{}, fmt.Errorf("%w: %q", ErrUnknownTemplate, name)
	}
	if data == nil {
		data = map[string]any{}
	}
	var subj, txt bytes.Buffer
	if err := t.subject[name].Execute(&subj, data); err != nil {
		return Email{}, fmt.Errorf("template %s (sujet): %w", name, err)
	}
	if err := t.text[name].Execute(&txt, data); err != nil {
		return Email{}, fmt.Errorf("template %s (texte): %w", name, err)
	}
	e := Email{Subject: strings.TrimSpace(subj.String()), Text: txt.String()}
	if ht := t.html[name]; ht != nil {
		var h bytes.Buffer
		if err := ht.ExecuteTemplate(&h, "layout", data); err != nil {
			return Email{}, fmt.Errorf("template %s (html): %w", name, err)
		}
		e.HTML = h.String()
	}
	return e, nil
}
