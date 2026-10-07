# zonefile-go

A DNS zone file generator written in Go. Reads a configuration file in an
OpenBSD-style syntax and generates zone data for Unbound or NSD, and the
`dhcpd.conf` for the dhcpd of OpenBSD, so that names, addresses and MAC
addresses are kept in one place.

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
zonefile-go [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd|dhcpd]
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

With `-t dhcpd`, zonefile-go writes a complete `dhcpd.conf` from the `dhcp`
blocks of the configuration, with a fixed address for every host with a
`mac` that turns on `dhcp`, on the host or for its whole zone:

```
dhcp 192.168.21.0/24 {
	range .100 .199
	router .1
	dns-server .1
	domain example.com
	lease 1d
}

zone example.com {
	network 192.168.21.0/24
	host printer .12 mac 00:00:5e:00:53:12 dhcp
}
```

A zone gets a new SOA serial only when its content changes: the serial
file (default `/var/db/zonefile-go.serial`) keeps the serial of each zone
together with a hash of its NSD zone file without the serial. A new serial
is `YYYYMMDDnn` (UTC date, counting up within a day) unless the zone sets
`serial`. The serial file is updated only after the output has been
written; keep it, since it is the only record of the serials.

## Building

```
make                  # zonefile-go for this system
make build-openbsd    # zonefile-go-openbsd-amd64
make build-all        # OpenBSD, FreeBSD and Linux binaries
make test             # go vet and the tests with -race
make install          # to /usr/local; PREFIX, DESTDIR and MANDIR are honoured
make man              # check and show the manual pages
make dist-openbsd     # dist/zonefile-go-VERSION-openbsd-amd64.tgz
make dist-freebsd     # dist/zonefile-go-VERSION-freebsd-amd64.tgz
```

The `dist` archives can be built on any system, e.g. a Mac, and hold the
binary, both manual pages and the example configuration in the layout of
`/usr/local`, owned by root. `DIST_ARCH=arm64` builds them for arm64.
On the server:

```
# tar -xzf zonefile-go-VERSION-openbsd-amd64.tgz -C /usr/local
# makewhatis /usr/local/man
```

On FreeBSD the manual pages go to `/usr/local/share/man`, and the default
configuration file is `/usr/local/etc/zonefile.conf` instead of
`/etc/zonefile.conf`, in the binary and in the manual pages.

The manual pages are [zonefile-go(8)](zonefile-go.8) and
[zonefile.conf(5)](zonefile.conf.5).

## License

MIT, see [LICENSE](LICENSE).
