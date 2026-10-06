# zonefile-go

A DNS zone file generator written in Go. Reads a configuration file in an
OpenBSD-style syntax and generates zone data for Unbound or NSD.

It is the successor of [zonefile](https://github.com/isnogudus/zonefile)
(Python) and [zonefile-rs](https://github.com/isnogudus/zonefile-rs)
(Rust) and aims to produce the same output.

**Status:** usable, but young. The configuration language is specified in
[docs/grammar.md](docs/grammar.md). For the configuration in
[examples/zones.conf](examples/zones.conf), the output matches that of
zonefile-rs except for these intended differences:

- records are written in a fixed order (zonefile-rs writes SRV and CNAME
  records and zones.conf in hash order),
- the Unbound CNAME lines contain the class (`IN CNAME`),
- identical records are rejected instead of being written twice.

## Example

```
email admin@example.com
nameserver ns1.example.com.

reverse 192.168.1.0/24

zone example.com {
	mx mail priority 10

	host @    192.168.1.1
	host www  192.168.1.2
	host mail { 192.168.1.3 2001:db8::3 } ttl 1h

	cname webmail mail
	srv _http._tcp www port 80 priority 0 weight 5
}
```

A larger example is in [examples/zones.conf](examples/zones.conf).

## Usage

```
zonefile-go [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd]
```

```
zonefile-go -n -f zones.conf                  # check only
zonefile-go -f zones.conf -o unbound-zones.conf
zonefile-go -f zones.conf -t nsd -o /var/nsd/zones
```

With `-t nsd`, zone files of zones that were removed from the configuration
are deleted from `master/`. Only files named in the previous `zones.conf`
are considered, so zone files maintained by hand in the same directory are
left alone.

The SOA serial is `YYYYMMDDnn` (UTC date, counting up within a day) unless
a zone sets `serial`. The last serial is kept in the serial file, which
is only updated after the output has been written.

## Building

```
make                  # zonefile-go for this system
make build-openbsd    # zonefile-go-openbsd-amd64
make build-all        # OpenBSD, FreeBSD and Linux binaries
make test             # go vet and the tests with -race
make install          # to /usr/local; PREFIX, DESTDIR and MANDIR are honoured
make man              # check and show the manual pages
```

The manual pages are [zonefile-go(8)](zonefile-go.8) and
[zonefile.conf(5)](zonefile.conf.5).

## License

MIT, see [LICENSE](LICENSE).
