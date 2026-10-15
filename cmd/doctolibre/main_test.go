package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args   []string
		code   int
		outHas string
		errHas string
	}{
		{[]string{"version"}, 0, version, ""},
		{[]string{"migrate"}, 2, "", "not implemented"},
		{[]string{"create-user"}, 2, "", "not implemented"},
		{[]string{"nope"}, 2, "", "inconnue"},
		{nil, 2, "", "usage"},
	}
	for _, tt := range tests {
		var out, errb bytes.Buffer
		if got := run(tt.args, &out, &errb); got != tt.code {
			t.Errorf("%v: code %d, attendu %d", tt.args, got, tt.code)
		}
		if !strings.Contains(out.String(), tt.outHas) || !strings.Contains(errb.String(), tt.errHas) {
			t.Errorf("%v: sortie inattendue %q / %q", tt.args, out.String(), errb.String())
		}
	}
}
