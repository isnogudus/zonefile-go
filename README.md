# zonefile-go

A DNS zone file generator written in Go. Reads a configuration file in an
OpenBSD-style syntax and generates zone data for Unbound or NSD.

It is the successor of [zonefile](https://github.com/isnogudus/zonefile)
(Python) and [zonefile-rs](https://github.com/isnogudus/zonefile-rs)
(Rust) and aims to produce the same output.

**Status:** early development. The configuration language is specified in
[docs/grammar.md](docs/grammar.md); the parser and the output generators
are not implemented yet.

## Example

```
set email admin@example.com
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

## Building

```
go build
```

## License

MIT, see [LICENSE](LICENSE).
