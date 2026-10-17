package notify

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"

	"doctolibre/internal/notify/sms"
)

// ErrForbiddenHost : le fournisseur d'un cabinet vise une adresse interne
// (bouclage, réseau privé, link-local, non routable). Voir DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS.
var ErrForbiddenHost = errors.New("notify: adresse de destination interdite (réseau interne)")

// forbiddenPrefixes complète les tests de netip (CGNAT, réservés, benchmarking…).
var forbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// forbiddenIP indique si a n'est pas une destination publique.
func forbiddenIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Control retourne le hook net.Dialer.Control qui refuse, à la connexion, les
// adresses IP internes : il voit l'IP réellement résolue (pas seulement le nom
// saisi), ce qui couvre le rebinding DNS. allowPrivate=true => pas de contrôle.
func Control(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	if allowPrivate {
		return nil
	}
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrForbiddenHost
		}
		a, err := netip.ParseAddr(host)
		if err != nil || forbiddenIP(a) {
			return ErrForbiddenHost
		}
		return nil
	}
}

// GuardedHTTPClient : client HTTP des drivers SMS d'un cabinet. Contrôle de
// l'IP à la connexion, pas de proxy d'environnement (il contournerait le
// contrôle), aucune redirection suivie.
func GuardedHTTPClient(allowPrivate bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = (&net.Dialer{Timeout: defaultDialTimeout, Control: Control(allowPrivate)}).DialContext
	return &http.Client{
		Timeout:       sms.DefaultTimeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
