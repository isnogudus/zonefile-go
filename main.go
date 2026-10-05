// Command zonefile-go generates Unbound and NSD zone data from a
// zonefile.conf configuration file.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/isnogudus/zonefile-go/internal/atomicfile"
	"github.com/isnogudus/zonefile-go/internal/config"
	"github.com/isnogudus/zonefile-go/internal/output"
	"github.com/isnogudus/zonefile-go/internal/serial"
	"github.com/isnogudus/zonefile-go/internal/zone"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr,
		"usage: %s [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd]\n",
		os.Args[0])
	os.Exit(1)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	var (
		file       = flag.String("f", "/etc/zonefile.conf", "configuration file, - for stdin")
		checkOnly  = flag.Bool("n", false, "check the configuration only")
		outPath    = flag.String("o", "", "output file (unbound, default stdout) or directory (nsd, default nsd)")
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

	cfg, err := config.ParseFile(*file)
	if err != nil {
		fatal(err)
	}
	zones, err := zone.Resolve(cfg)
	if err != nil {
		fatal(err)
	}
	if *checkOnly {
		fmt.Fprintln(os.Stderr, "configuration OK")
		return
	}

	old, err := serial.Load(*serialFile)
	if err != nil {
		fatal(err)
	}
	next := serial.Next(old, time.Now())

	switch *format {
	case "unbound":
		data := output.Unbound(zones, next)
		if *outPath == "" {
			_, err = os.Stdout.Write(data)
		} else {
			err = atomicfile.WriteFile(*outPath, data, 0o644)
		}
	case "nsd":
		dir := *outPath
		if dir == "" {
			dir = "nsd"
		}
		var removed []string
		removed, err = output.WriteNSD(dir, zones, next)
		for _, file := range removed {
			fmt.Fprintf(os.Stderr, "removed %s\n", filepath.Join(dir, file))
		}
	}
	if err != nil {
		fatal(err)
	}

	// Only store the serial once the zones have been written.
	if err := serial.Save(*serialFile, next); err != nil {
		fatal(err)
	}
}
