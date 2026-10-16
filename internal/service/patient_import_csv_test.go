package service_test

import (
	"fmt"
	"strings"
	"testing"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

func TestParseImportCSV(t *testing.T) {
	t.Parallel()
	// « Prénom » et « é » en Windows-1252 : 0xE9 ; « € » : 0x80.
	cp1252 := []byte("N\xe9e;Nom;Pr\xe9nom;T\xe9l\xe9phone;Adresse\n" +
		"x;Gu\xe9rin;Ren\xe9e;06 12 34 56 78;3 rue de l'\xe9glise \x80\n")
	bom := append([]byte{0xEF, 0xBB, 0xBF}, []byte("NOM,PRENOM,Courriel,Date de naissance\nÉlodie,Martin,Elodie@Example.test,03/04/1985\n")...)
	// ci-dessus, « Élodie » est dans la colonne Nom (valeur), Martin dans Prénom : peu importe.

	tests := []struct {
		name string
		in   []byte
		want ImportWant
	}{
		{"cp1252 point-virgule", cp1252, ImportWant{last: "Guérin", first: "Renée", phone: "06 12 34 56 78", address: "3 rue de l'église €"}},
		{"BOM UTF-8 virgule, accents et casse", bom, ImportWant{last: "Élodie", first: "Martin", email: "Elodie@Example.test", birth: "1985-04-03"}},
		{"en-têtes avec accents et synonymes", []byte("Prénom;NOM DE FAMILLE;Portable;E-mail;Né(e) le\nZoé;Lefèvre;+33 6 11 22 33 44;z@x.test;2001-02-03\n"),
			ImportWant{last: "Lefèvre", first: "Zoé", phone: "+33 6 11 22 33 44", email: "z@x.test", birth: "2001-02-03"}},
		{"champ entre guillemets avec séparateur et CRLF", []byte("nom;prenom;tel\r\n\"Du;pont\";\"Jean \"\"JJ\"\"\";0612345678\r\n"),
			ImportWant{last: "Du;pont", first: `Jean "JJ"`, phone: "0612345678"}},
		{"portable prioritaire sur le fixe", []byte("nom;prenom;telephone;mobile\nA;B;01 02 03 04 05;06 12 34 56 78\n"),
			ImportWant{last: "A", first: "B", phone: "06 12 34 56 78"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows, err := service.ParseImportCSV(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("%d lignes", len(rows))
			}
			r := rows[0]
			if r.LastName != tt.want.last || r.FirstName != tt.want.first || r.Phone != tt.want.phone ||
				r.Email != tt.want.email || r.Birth != tt.want.birth || r.Address != tt.want.address {
				t.Errorf("got %+v, want %+v", r, tt.want)
			}
			if r.Line != 2 {
				t.Errorf("ligne = %d", r.Line)
			}
		})
	}
}

type ImportWant struct{ last, first, phone, email, birth, address string }

func TestParseImportCSVBadDate(t *testing.T) {
	t.Parallel()
	rows, err := service.ParseImportCSV([]byte("nom;prenom;email;naissance\nA;B;a@x.test;31/02/2000\nC;D;c@x.test;5/1/1999\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].BirthBad || rows[1].Birth != "1999-01-05" {
		t.Errorf("dates: %+v", rows)
	}
}

func TestParseImportCSVRejects(t *testing.T) {
	t.Parallel()
	var many strings.Builder
	many.WriteString("nom;prenom;email\n")
	for i := 0; i <= service.MaxImportRows; i++ {
		fmt.Fprintf(&many, "N%d;P;p%d@x.test\n", i, i)
	}
	var exactly strings.Builder
	exactly.WriteString("nom;prenom;email\n")
	for i := 0; i < service.MaxImportRows; i++ {
		fmt.Fprintf(&exactly, "N%d;P;p%d@x.test\n", i, i)
	}
	tests := []struct {
		name string
		in   []byte
		ok   bool
	}{
		{"vide", nil, false},
		{"sans colonne prénom", []byte("nom;email\nA;a@x.test\n"), false},
		{"sans colonne nom", []byte("prenom;email\nA;a@x.test\n"), false},
		{"en-tête seul", []byte("nom;prenom;email\n"), false},
		{"plus de 2 Mo", append([]byte("nom;prenom;email\n"), make([]byte, service.MaxImportBytes)...), false},
		{"plus de 5000 lignes", []byte(many.String()), false},
		{"exactement 5000 lignes", []byte(exactly.String()), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := service.ParseImportCSV(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if err != nil && len(domain.FieldErrors(err)) == 0 {
				t.Errorf("erreur non métier : %v", err)
			}
		})
	}
}
