package notify

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

// builtMessage est un message MIME complet avec son enveloppe.
type builtMessage struct {
	Raw       []byte
	MessageID string
	From      string // adresse d'enveloppe
	To        string // adresse d'enveloppe
}

// reserved : en-têtes gérés par buildMessage, non surchargeables via Email.Headers.
var reserved = map[string]bool{
	"from": true, "to": true, "subject": true, "date": true, "message-id": true,
	"mime-version": true, "content-type": true, "content-transfer-encoding": true, "reply-to": true,
}

func hasCRLF(s string) bool { return strings.ContainsAny(s, "\r\n") }

// buildMessage construit le message MIME : en-têtes encodés RFC 2047,
// text/plain seul ou multipart/alternative (texte puis HTML), corps en
// quoted-printable UTF-8.
func buildMessage(from string, e Email, now time.Time) (builtMessage, error) {
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return builtMessage{}, fmt.Errorf("%w: expéditeur", ErrInvalidEmail)
	}
	toAddr, err := mail.ParseAddress(e.To)
	if err != nil || hasCRLF(e.To) {
		return builtMessage{}, fmt.Errorf("%w: destinataire", ErrInvalidEmail)
	}
	if hasCRLF(e.Subject) {
		return builtMessage{}, fmt.Errorf("%w: sujet", ErrInvalidEmail)
	}
	if strings.TrimSpace(e.Text) == "" {
		return builtMessage{}, fmt.Errorf("%w: corps texte vide", ErrInvalidEmail)
	}
	var replyTo *mail.Address
	if e.ReplyTo != "" {
		if replyTo, err = mail.ParseAddress(e.ReplyTo); err != nil || hasCRLF(e.ReplyTo) {
			return builtMessage{}, fmt.Errorf("%w: reply-to", ErrInvalidEmail)
		}
	}

	id, err := newMessageID(fromAddr.Address)
	if err != nil {
		return builtMessage{}, err
	}

	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", fromAddr.String())
	h("To", toAddr.String())
	if replyTo != nil {
		h("Reply-To", replyTo.String())
	}
	h("Subject", mime.QEncoding.Encode("utf-8", e.Subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", id)
	h("MIME-Version", "1.0")
	keys := make([]string, 0, len(e.Headers))
	for k := range e.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := e.Headers[k]
		if reserved[strings.ToLower(k)] || hasCRLF(k) || hasCRLF(v) || strings.ContainsAny(k, ": ") || k == "" {
			return builtMessage{}, fmt.Errorf("%w: en-tête %q", ErrInvalidEmail, k)
		}
		h(k, v)
	}

	if e.HTML == "" {
		h("Content-Type", "text/plain; charset=utf-8")
		h("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, e.Text); err != nil {
			return builtMessage{}, err
		}
	} else {
		mw := multipart.NewWriter(&b)
		h("Content-Type", "multipart/alternative; boundary="+mw.Boundary())
		b.WriteString("\r\n")
		for _, p := range []struct{ typ, body string }{{"text/plain", e.Text}, {"text/html", e.HTML}} {
			pw, err := mw.CreatePart(textproto.MIMEHeader{
				"Content-Type":              {p.typ + "; charset=utf-8"},
				"Content-Transfer-Encoding": {"quoted-printable"},
			})
			if err != nil {
				return builtMessage{}, fmt.Errorf("mime: %w", err)
			}
			if err := writeQP(pw, p.body); err != nil {
				return builtMessage{}, err
			}
		}
		if err := mw.Close(); err != nil {
			return builtMessage{}, fmt.Errorf("mime: %w", err)
		}
	}
	return builtMessage{Raw: b.Bytes(), MessageID: id, From: fromAddr.Address, To: toAddr.Address}, nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return fmt.Errorf("mime: %w", err)
	}
	if err := qp.Close(); err != nil {
		return fmt.Errorf("mime: %w", err)
	}
	return nil
}

func newMessageID(fromAddr string) (string, error) {
	domain := "localhost"
	if _, d, ok := strings.Cut(fromAddr, "@"); ok && d != "" {
		domain = d
	}
	r := make([]byte, 16)
	if _, err := rand.Read(r); err != nil {
		return "", fmt.Errorf("message-id: %w", err)
	}
	return "<" + hex.EncodeToString(r) + "@" + domain + ">", nil
}
