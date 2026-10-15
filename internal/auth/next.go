package auth

import (
	"net/url"
	"strings"
)

// DefaultNext est la destination après connexion quand next est absent ou refusé.
const DefaultNext = "/admin"

func queryEscape(s string) string { return url.QueryEscape(s) }

// SafeNext valide la destination post-connexion : uniquement un chemin relatif
// du site (« /… »), jamais une URL absolue, protocol-relative (« //hôte »),
// avec antislash ou caractère de contrôle. Sinon DefaultNext.
func SafeNext(next string) string {
	if next == "" || len(next) > 2048 || next[0] != '/' {
		return DefaultNext
	}
	if strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\") {
		return DefaultNext
	}
	for _, c := range next {
		if c < 0x20 || c == 0x7f {
			return DefaultNext
		}
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || !strings.HasPrefix(u.Path, "/") {
		return DefaultNext
	}
	return next
}
