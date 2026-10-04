# zonefile.conf — configuration grammar

Status: **draft**. This document describes the configuration language read
by `zonefile-go`. It covers the feature set of `zonefile-rs` v0.2.1 and is
modelled on OpenBSD configuration files such as `pf.conf(5)`,
`httpd.conf(5)` and `relayd.conf(5)`: one statement per line, keywords
instead of punctuation, curly braces for blocks and lists, macros, and
`include`.

## Example

```
# Macros
ns1  = "ns1.example.com."
mail = "mail.example.com."
lan  = "{ 192.168.21.0/24 fd00:1234:5678:1000::/64 }"

# Global options
set email admin@example.com
set ttl 3h

nameserver $ns1

reverse { 192.168.0.0/16 fd00:1234:5678:1000::/64 }

zone example.com {
	network $lan
	mx mail priority 10

	host router  .1 alias { @ dns ntp www }
	host printer .12 no inet6
	host gateway { 203.0.113.1 2001:db8::1 } no ptr

	cname webmail mail
	srv _mqtt._tcp mqtt port 1883
}

zone dmz.example.com {
	mx $mail priority 10
	host git 192.168.114.100
}
```

A complete example that mirrors `zonefile-rs/zones.yaml` is in
[`examples/zones.conf`](../examples/zones.conf).

## Lexical structure

- **Comments** start with `#` and run to the end of the line.
- **Statements** end at a newline. A backslash at the very end of a line
  continues the statement on the next line.
- **Newlines inside `{ }` lists** are ignored, so long lists can span
  several lines without backslashes. Newlines inside `{ }` *blocks*
  (`zone`, `reverse`) separate statements.
- **Words** are runs of characters other than whitespace, `#`, `{`, `}`,
  `"`, `=` and `,`. That covers host names, `@`, `*`, IPv4/IPv6 addresses,
  CIDR networks, e-mail addresses, numbers and durations without quoting.
- **Address suffixes** are words starting with a dot followed by one or
  more decimal octets, e.g. `.37` or `.21.37`. They are resolved against
  the networks of the zone (see [network](#network)).
- **Strings** are enclosed in double quotes. `\"` and `\\` are the only
  escapes. A string is always a value, never a keyword, so a host called
  `ttl` can be written as `host "ttl" …`.
- **Keywords** are recognised only where the grammar expects one. In a
  name or address position, `port` or `alias` is just a word.
- **Commas** between list items are optional, as in `pf.conf`:
  `{ a, b }` and `{ a b }` mean the same thing.

## Macros

```
name = value
```

`value` is a word, a string or a `{ }` list. As in `pf.conf`, the contents
of a quoted value are split into tokens again, so `lan = "{ a b }"` defines
a list and macros inside it are expanded at definition time. The macro is
used as `$name`,
and the reference must form a complete token: `$prefix::1` is **not**
expanded. Address prefixes are handled by [network](#network) instead. A macro must be defined
before it is used. A list macro may appear wherever a list is allowed, and
a single value wherever a single item is allowed. Macro names match
`[A-Za-z_][A-Za-z0-9_]*` and must not be keywords.

## Include

```
include "path"
```

Reads another file at this point. A relative path is resolved against the
directory of the including file. `include` is allowed only at top level.

## Grammar

EBNF. Terminals are quoted. `NL` is a newline that ends a statement.

```
config        = { [ toplevel ] NL } .
toplevel      = macro | include | set | nameserver | mx | zone | reverse .

macro         = MACRONAME "=" value .
include       = "include" STRING .

set           = "set" option .
option        = "email"        EMAIL
              | "ttl"          duration
              | "refresh"      duration
              | "retry"        duration
              | "expire"       duration
              | "negative-ttl" duration
              | "serial"       NUMBER
              | "mx-priority"  NUMBER
              | "srv-priority" NUMBER
              | "srv-weight"   NUMBER
              | "ptr"          ( "yes" | "no" ) .

nameserver    = "nameserver" name-list [ "ttl" duration ] .
mx            = "mx" name { mx-opt } .
mx-opt        = "priority" NUMBER | "ttl" duration .

zone          = "zone" name "{" NL { [ zone-stmt ] NL } "}" .
zone-stmt     = set | network | nameserver | mx | host | cname | srv .

network       = "network" net-list .

host          = "host" name host-addrs { host-opt } .
host-addrs    = host-addr | "{" host-addr { [ "," ] host-addr } "}" .
host-addr     = ADDRESS | SUFFIX .
host-opt      = "alias" name-list
              | "ttl" duration
              | "ptr"
              | "no" "ptr"
              | "no" "inet"
              | "no" "inet6" .

cname         = "cname" name name [ "ttl" duration ] .

srv           = "srv" SRVNAME name "port" NUMBER { srv-opt } .
srv-opt       = "priority" NUMBER | "weight" NUMBER | "ttl" duration .

reverse       = "reverse" net-list [ "{" NL { [ reverse-stmt ] NL } "}" ] .
reverse-stmt  = set | nameserver .

addr-list     = ADDRESS | "{" ADDRESS { [ "," ] ADDRESS } "}" .
name-list     = name    | "{" name    { [ "," ] name    } "}" .
net-list      = NETWORK | "{" NETWORK { [ "," ] NETWORK } "}" .

name          = WORD | STRING .
value         = WORD | STRING | "{" item { [ "," ] item } "}" .
duration      = NUMBER [ "s" | "m" | "h" | "d" | "w" ] .
SUFFIX        = "." OCTET { "." OCTET } .
```

The opening `{` of a block has to be on the same line as its keyword.
Options of `host`, `mx`, `srv` and `nameserver` can be given in any order,
but each only once.

## Statements

### set

`set` changes a default for everything that follows in the same scope.

| Option         | Default  | Top level | `zone` | `reverse` | Meaning                                  |
|----------------|----------|:---------:|:------:|:---------:|------------------------------------------|
| `email`        | —        | ✓         | ✓      | ✓         | SOA contact (`RNAME`); required          |
| `ttl`          | `3h`     | ✓         | ✓      | ✓         | default TTL of the zone and its records  |
| `refresh`      | `2h`     | ✓         | ✓      | ✓         | SOA refresh                              |
| `retry`        | `1h`     | ✓         | ✓      | ✓         | SOA retry, must be less than `refresh`   |
| `expire`       | `2w`     | ✓         | ✓      | ✓         | SOA expire                               |
| `negative-ttl` | `1h`     | ✓         | ✓      | ✓         | SOA minimum (negative caching TTL)       |
| `serial`       | computed | ✓         | ✓      | ✓         | fixed serial instead of `YYYYMMDDnn`     |
| `mx-priority`  | `0`      | ✓         | ✓      |           | priority of `mx` without `priority`      |
| `srv-priority` | `5`      | ✓         | ✓      |           | priority of `srv` without `priority`     |
| `srv-weight`   | `10`     | ✓         | ✓      |           | weight of `srv` without `weight`         |
| `ptr`          | `yes`    | ✓         | ✓      |           | whether hosts get PTR records            |

Top-level `set` statements must come before the first `zone` or `reverse`
block. That way, reading the file from the top shows which defaults are in
effect. Inside a block, `set` must come before the first record.

The defaults in the table are the values of `zonefile-rs`
(`src/constants.rs`) written as durations: 10800 s = `3h`, 1209600 s = `2w`.

### nameserver, mx at top level

`nameserver` and `mx` at top level define the default NS and MX records
for every zone that has none of its own. Once a zone declares at least one
`nameserver` (or `mx`), the defaults of that type no longer apply to it. A
forward zone without any nameserver is an error. At top level, names must
be absolute (end in a dot), because there is no zone to resolve them
against.

### zone

```
zone example.com {
	…
}
```

Declares a forward zone. The trailing dot of the zone name is optional.

**Names inside a zone:**

- `name` relative to the zone → `name.example.com.`
- `name.` absolute, taken as is
- `@` the zone apex
- `*` wildcard (never gets a PTR record)

### network

```
network NETWORK|{ NETWORK … }
```

Declares the networks that address suffixes in this zone are resolved
against: at most one IPv4 and one IPv6 network per zone, otherwise a
suffix would be ambiguous. `network` must come before the first `host`.
Several zones can share the same networks through a list macro.

The declared networks also decide which address families a suffix
produces. A zone with only an IPv4 network gets only A records from
suffixes, and a zone with only an IPv6 network gets only AAAA records. In a
zone with both, a host that lacks one family says so with `no inet` or
`no inet6`; that is a property of the host, not of the zone.

`network` only says where the hosts of a zone live. It does **not** create
reverse zones and does not enable PTR records — that is the job of
[reverse](#reverse).

**Resolving a suffix:** a suffix such as `.37` is turned into one address
per network of the zone:

- **IPv4:** the octets replace the rightmost octets of the network:
  `.37` in `192.168.21.0/24` → `192.168.21.37`; `.21.37` in
  `192.168.0.0/16` → `192.168.21.37`.
- **IPv6:** the octets are read as one unsigned integer, and that value
  becomes the host part: `.37` → `::25`, `.200` → `::c8`, `.21.37` →
  `::1525`. In `fd00:1234:5678:1000::/64`, `.37` thus becomes
  `fd00:1234:5678:1000::25`.

This is the only mapping. The numeric value is kept, not the decimal
digits, so `.67` becomes `::43`, not `::67`.

It is an error if

- the zone has no `network` and a host uses a suffix,
- the suffix has more octets than the host part of the IPv4 network
  (`.1.2` in a /24), or the value does not fit into the host part of the
  IPv6 network,
- the resulting address is the network or broadcast address of an IPv4
  network.

### host

```
host NAME ADDRESS|SUFFIX|{ … } [alias NAME|{ NAME … }] [ttl D]
     [no ptr] [no inet] [no inet6]
```

Creates one A or AAAA record per address. A suffix expands to one address
per network of the zone (see [network](#network)); a full address is taken
as is. Both can be mixed in a list. `no inet` and `no inet6` drop the
addresses of that family, so `host printer .12 no inet6` is IPv4 only. `alias` names get the **same
addresses as additional A/AAAA records**, not CNAMEs, which matches
`zonefile-rs`. Only the host name itself gets a PTR record; aliases do
not. `no ptr` and `ptr` override `set ptr` for this host.

### cname

```
cname NAME TARGET [ttl D]
```

### srv

```
srv _service._proto TARGET port N [priority N] [weight N] [ttl D]
```

`SRVNAME` must consist of at least two labels, both starting with `_`.

### reverse

```
reverse NETWORK|{ NETWORK … } [{ … }]
```

Declares one reverse zone per network. The zone name is derived from the
network (`192.168.0.0/16` → `168.192.in-addr.arpa.`,
`fd00:1234:5678:1000::/64` → `…ip6.arpa.`). The optional block sets SOA
values and nameservers for these zones; without a nameserver of its own a
reverse zone uses the top-level ones. Nameservers in a `reverse` block must
be absolute.

Every address of a host with PTR enabled ends up in the reverse zone whose
network contains it. Addresses outside every reverse network are skipped.

Reverse zones are declared separately from `zone` and `network` on
purpose: `reverse` lists exactly the address ranges for which reverse
records are served. A host with an external address (say, a public
gateway address in a forward zone) therefore never produces a PTR in a zone
that is not ours, even without `no ptr`.

## Validation

The parser reports errors as `file:line: message`, as OpenBSD tools do.
After an error it skips the rest of the statement and continues, so one run
reports every syntax error:

```
zones.conf:14: unknown option "tll"
zones.conf:31: retry (2h) must be less than refresh (1h)
zones.conf:52: srv: "mqtt._tcp": service label must start with "_"
```

Checks taken over from `zonefile-rs`:

- TTL in 1 … 2147483647
- e-mail address per RFC 5322 (local part ≤ 64 characters, …)
- DNS names per RFC 1035 (≤ 253 characters, labels ≤ 63)
- `retry` < `refresh`
- SRV names `_service._proto`
- reverse networks must not overlap

New checks, which `zonefile-rs` does not enforce:

- **Prefix alignment:** the prefix length of a reverse network must be a
  multiple of 8 (IPv4) or 4 (IPv6). `zonefile-rs` silently rounds down.
- **Unique PTRs:** if two hosts would produce a PTR for the same address,
  that is an error. `zonefile-rs` silently keeps one of them.
- **Address suffixes:** see [network](#network).
- **Duplicates:** a `zone` must not be declared twice, a host name must not
  appear twice in a zone, and a CNAME must not collide with another name.

## Command line (sketch)

Modelled on OpenBSD daemons:

```
zonefile-go [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd]
```

| Flag | Meaning                                                       |
|------|---------------------------------------------------------------|
| `-f` | configuration file (default `/etc/zonefile.conf`, `-` = stdin) |
| `-n` | check the configuration only, write nothing (like `pfctl -n`)  |
| `-o` | output file (unbound) or directory (nsd); default stdout       |
| `-s` | serial file (default `.serial`)                                |
| `-t` | output format, `unbound` (default) or `nsd`                    |
| `-V` | print the version                                              |

## Mapping from zonefile-rs

| zonefile-rs (YAML/TOML)            | zonefile.conf                         |
|------------------------------------|---------------------------------------|
| `defaults.email`                   | `set email …`                         |
| `defaults.ttl` / `refresh` / …     | `set ttl …` / `set refresh …` / …     |
| `defaults.nrc-ttl`                 | `set negative-ttl …`                  |
| `defaults.mx-prio`                 | `set mx-priority …`                   |
| `defaults.srv-prio` / `srv-weight` | `set srv-priority …` / `set srv-weight …` |
| `defaults.with-ptr: false`         | `set ptr no`                          |
| `defaults.nameserver`              | top-level `nameserver …`              |
| `defaults.mx`                      | top-level `mx …`                      |
| `zone.<name>`                      | `zone <name> { … }`                   |
| `hosts.<h>: ip`                    | `host <h> ip` or `host <h> .suffix`   |
| `hosts.<h>: {ip, alias, ttl}`      | `host <h> {…} alias {…} ttl …`        |
| `hosts.<h>.with-ptr: false`        | `host <h> … no ptr`                   |
| `cname.<n>: target`                | `cname <n> target`                    |
| `srv.<s>: {target, port, …}`       | `srv <s> target port … priority …`    |
| `reverse: [nets]`                  | `reverse { nets }`                    |
| `reverse.<net>: {options}`         | `reverse <net> { set … }`             |

## Open questions

1. **Duration suffixes in the output:** the output always writes seconds,
   so the zone files stay byte-for-byte comparable with `zonefile-rs`.
2. **Migration aid:** a subcommand that converts existing TOML/YAML into
   this syntax?
3. **Defaults for `/etc`:** is `/etc/zonefile.conf` the right default path,
   or should the file have to be given with `-f`?
