package secretbox_test

import (
	"bytes"
	"errors"
	"testing"

	"doctolibre/internal/secretbox"
)

func TestRoundTripAndBinding(t *testing.T) {
	t.Parallel()
	b, err := secretbox.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal([]byte("mot-de-passe"), []byte("cabinet-A|email"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("mot-de-passe")) {
		t.Fatal("le chiffré contient le clair")
	}
	got, err := b.Open(sealed, []byte("cabinet-A|email"))
	if err != nil || string(got) != "mot-de-passe" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	for name, aad := range map[string]string{"autre cabinet": "cabinet-B|email", "autre canal": "cabinet-A|sms"} {
		if _, err := b.Open(sealed, []byte(aad)); !errors.Is(err, secretbox.ErrDecrypt) {
			t.Errorf("%s : err = %v", name, err)
		}
	}
	other, _ := secretbox.New("another-secret-key-another-secret-key")
	if _, err := other.Open(sealed, []byte("cabinet-A|email")); !errors.Is(err, secretbox.ErrDecrypt) {
		t.Errorf("autre clé : err = %v", err)
	}
	if _, err := b.Open([]byte("court"), nil); !errors.Is(err, secretbox.ErrDecrypt) {
		t.Errorf("trop court : err = %v", err)
	}
	if _, err := secretbox.New(""); err == nil {
		t.Error("clé vide acceptée")
	}
}
