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
		"usage: %s [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd|dhcpd]\n",
		os.Args[0])
	os.Exit(1)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	var (
		file       = flag.String("f", defaultConfig, "configuration file, - for stdin")
		checkOnly  = flag.Bool("n", false, "check the configuration only")
		outPath    = flag.String("o", "", "output file (unbound, dhcpd; default stdout) or directory (nsd, default nsd)")
		serialFile = flag.String("s", "/var/db/zonefile-go.serial", "file with the serial and hash of each zone")
		format     = flag.String("t", "unbound", "output format: unbound, nsd or dhcpd")
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
	case "unbound", "nsd", "dhcpd":
	default:
		fmt.Fprintf(os.Stderr, "zonefile-go: unknown output format %q\n", *format)
		os.Exit(1)
	}

	cfg, err := config.ParseFile(*file)
	if err != nil {
		fatal(err)
	}
	res, err := zone.Resolve(cfg)
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, w)
	}
	if err != nil {
		fatal(err)
	}
	zones := res.Zones
	if *checkOnly {
		for _, n := range res.Notes {
			fmt.Fprintln(os.Stderr, n)
		}
		fmt.Fprintln(os.Stderr, "configuration OK")
		return
	}

	// dhcpd needs no serials, so the serial file is left alone.
	if *format == "dhcpd" {
		if len(res.DHCP) == 0 {
			fatal(fmt.Errorf("%s: no dhcp blocks", *file))
		}
		if err := writeFile(*outPath, output.Dhcpd(res.DHCPGlobal, res.DHCP)); err != nil {
			fatal(err)
		}
		return
	}

	prev, err := serial.Load(*serialFile)
	if err != nil {
		fatal(err)
	}
	state := serial.Assign(zones, prev, output.ZoneHash, time.Now())

	switch *format {
	case "unbound":
		err = writeFile(*outPath, output.Unbound(zones))
	case "nsd":
		dir := *outPath
		if dir == "" {
			dir = "nsd"
		}
		var removed []string
		removed, err = output.WriteNSD(dir, zones)
		for _, file := range removed {
			fmt.Fprintf(os.Stderr, "removed %s\n", filepath.Join(dir, file))
		}
	}
	if err != nil {
		fatal(err)
	}

	// Only store the serials once the zones have been written.
	if err := serial.Save(*serialFile, state); err != nil {
		fatal(err)
	}
}

// writeFile writes data atomically to path, or to standard output if path
// is empty.
func writeFile(path string, data []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return atomicfile.WriteFile(path, data, 0o644)
}
