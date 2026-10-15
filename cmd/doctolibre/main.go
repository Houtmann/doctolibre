// Commande doctolibre : serveur de prise de rendez-vous.
package main

import (
	"fmt"
	"io"
	"os"
)

// version est injectée au build : -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "serve", "migrate", "create-user":
		_, _ = fmt.Fprintf(stderr, "doctolibre %s: not implemented\n", args[0])
		return 2
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "doctolibre: commande inconnue %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: doctolibre <serve|migrate|create-user|version>")
}
