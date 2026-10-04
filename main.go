// Command zonefile-go generates Unbound and NSD zone data from a
// zonefile.conf configuration file.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr,
		"usage: %s [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd]\n",
		os.Args[0])
	os.Exit(1)
}

func main() {
	var (
		file       = flag.String("f", "/etc/zonefile.conf", "configuration file, - for stdin")
		checkOnly  = flag.Bool("n", false, "check the configuration only")
		output     = flag.String("o", "", "output file (unbound) or directory (nsd)")
		serialFile = flag.String("s", ".serial", "serial number file")
		format     = flag.String("t", "unbound", "output format: unbound or nsd")
		showVer    = flag.Bool("V", false, "print version and exit")
	)
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 0 {
		usage()
	}

	if *showVer {
		fmt.Println("zonefile-go", version)
		return
	}

	switch *format {
	case "unbound", "nsd":
	default:
		fmt.Fprintf(os.Stderr, "zonefile-go: unknown output format %q\n", *format)
		os.Exit(1)
	}

	_, _, _, _ = *file, *checkOnly, *output, *serialFile
	fmt.Fprintln(os.Stderr, "zonefile-go: not implemented yet, see docs/grammar.md")
	os.Exit(1)
}
