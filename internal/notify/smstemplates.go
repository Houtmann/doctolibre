package notify

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	ttemplate "text/template"
)

//go:embed templates/sms/*.tmpl
var smsFS embed.FS

// RenderSMS rend le SMS name (templates/sms/<name>.tmpl) avec data. Texte brut :
// aucun échappement. Une clé absente est une erreur. Le texte est visé en un
// seul segment GSM-7 (160 caractères) : le service tronque le nom du cabinet.
func RenderSMS(name string, data map[string]any) (string, error) {
	body, err := smsFS.ReadFile("templates/sms/" + name + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("%w: sms %q", ErrUnknownTemplate, name)
	}
	t, err := ttemplate.New(name).Option("missingkey=error").Parse(string(body))
	if err != nil {
		return "", fmt.Errorf("template sms %s: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("template sms %s: %w", name, err)
	}
	return strings.TrimSpace(b.String()), nil
}
