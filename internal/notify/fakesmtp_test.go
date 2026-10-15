package notify_test

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// received est un message reçu par le faux serveur SMTP.
type received struct {
	From, To string
	Data     string
	AuthUser string
	AuthPass string
	TLS      bool
}

// fakeSMTP est un serveur SMTP minimal en mémoire (EHLO, STARTTLS, AUTH PLAIN
// et LOGIN, MAIL, RCPT, DATA, QUIT).
type fakeSMTP struct {
	ln        net.Listener
	tlsConf   *tls.Config // non nil : STARTTLS annoncé ; avec implicit, TLS dès l'accept
	implicit  bool
	authMechs string // "" = pas d'AUTH annoncé ; ex. "PLAIN LOGIN"
	rcptReply string // réponse à RCPT ; défaut "250 ok"
	mu        sync.Mutex
	msgs      []received
}

// testCert retourne la config TLS du serveur et celle du client (qui fait
// confiance au certificat auto-signé de httptest, valable pour 127.0.0.1).
func testCert(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{Certificates: srv.TLS.Certificates, MinVersion: tls.VersionTLS12},
		&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func newFakeSMTP(t *testing.T, f *fakeSMTP) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) messages() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.msgs...)
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	isTLS := false
	if f.implicit {
		tc := tls.Server(c, f.tlsConf)
		if tc.Handshake() != nil {
			return
		}
		c, isTLS = tc, true
	}
	br := bufio.NewReader(c)
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(c, format+"\r\n", a...) }
	say("220 fake ESMTP")
	var cur received
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			caps := []string{"fake"}
			if f.tlsConf != nil && !isTLS {
				caps = append(caps, "STARTTLS")
			}
			if f.authMechs != "" {
				caps = append(caps, "AUTH "+f.authMechs)
			}
			caps = append(caps, "8BITMIME")
			for i, cp := range caps {
				sep := "-"
				if i == len(caps)-1 {
					sep = " "
				}
				say("250%s%s", sep, cp)
			}
		case "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, f.tlsConf)
			if tc.Handshake() != nil {
				return
			}
			c, isTLS = tc, true
			br = bufio.NewReader(c)
		case "AUTH":
			mech, initial, _ := strings.Cut(arg, " ")
			switch strings.ToUpper(mech) {
			case "PLAIN":
				if initial == "" {
					say("334 ")
					initial, _ = br.ReadString('\n')
					initial = strings.TrimSpace(initial)
				}
				raw, _ := base64.StdEncoding.DecodeString(initial)
				parts := strings.Split(string(raw), "\x00")
				if len(parts) == 3 {
					cur.AuthUser, cur.AuthPass = parts[1], parts[2]
				}
			case "LOGIN":
				say("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
				u, _ := br.ReadString('\n')
				say("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
				p, _ := br.ReadString('\n')
				ub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
				pb, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
				cur.AuthUser, cur.AuthPass = string(ub), string(pb)
			default:
				say("504 mechanism not supported")
				continue
			}
			say("235 ok")
		case "MAIL":
			cur.From = between(arg)
			say("250 ok")
		case "RCPT":
			cur.To = between(arg)
			if f.rcptReply != "" {
				say("%s", f.rcptReply)
				continue
			}
			say("250 ok")
		case "DATA":
			say("354 end with <CRLF>.<CRLF>")
			var sb strings.Builder
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				sb.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.Data, cur.TLS = sb.String(), isTLS
			f.mu.Lock()
			f.msgs = append(f.msgs, cur)
			f.mu.Unlock()
			cur = received{AuthUser: cur.AuthUser, AuthPass: cur.AuthPass}
			say("250 queued")
		case "RSET", "NOOP":
			say("250 ok")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func between(s string) string {
	i, j := strings.Index(s, "<"), strings.LastIndex(s, ">")
	if i < 0 || j < i {
		return s
	}
	return s[i+1 : j]
}
