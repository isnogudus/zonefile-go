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
email admin@example.com
ttl 3h

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
toplevel      = macro | include | setting | ptr | nameserver | mx | zone
              | reverse | dhcp .

macro         = MACRONAME "=" value .
include       = "include" STRING .

setting       = "email"        EMAIL
              | "ttl"          duration
              | "refresh"      duration
              | "retry"        duration
              | "expire"       duration
              | "negative-ttl" duration
              | "serial"       NUMBER .

ptr           = "ptr" | "no" "ptr" .

nameserver    = "nameserver" name-list [ "ttl" duration ] .
mx            = "mx" name { mx-opt } .
mx-opt        = "priority" NUMBER | "ttl" duration .

zone          = "zone" name "{" NL { [ zone-stmt ] NL } "}" .
zone-stmt     = setting | ptr | "no" "mx" | dhcp-switch | dhcp-profile
              | network | nameserver | mx | host | cname | srv .
dhcp-switch   = "dhcp" | "no" "dhcp" .
dhcp-profile  = "dhcp-profile" name .

network       = "network" net-list .

host          = "host" name host-addrs { host-opt }
                [ "{" NL { [ host-opt { host-opt } ] NL } "}" ] .
host-addrs    = host-addr | "{" host-addr { [ "," ] host-addr } "}" .
host-addr     = ADDRESS | SUFFIX .
host-opt      = "alias" name-list
              | "ttl" duration
              | "ptr"
              | "no" "ptr"
              | "no" "inet"
              | "no" "inet6"
              | "mac" mac-list
              | "dhcp"
              | "no" "dhcp"
              | "dhcp-profile" name .
mac-list      = MAC | "{" MAC { [ "," ] MAC } "}" .

cname         = "cname" name name [ "ttl" duration ] .

srv           = "srv" SRVNAME name "port" NUMBER { srv-opt } .
srv-opt       = "priority" NUMBER | "weight" NUMBER | "ttl" duration .

reverse       = "reverse" net-list [ "{" NL { [ reverse-stmt ] NL } "}" ] .
reverse-stmt  = setting | nameserver .

dhcp          = "dhcp" [ NETWORK ] "{" NL { [ dhcp-stmt ] NL } "}" .
dhcp-stmt     = "range" host-addr host-addr
              | "server-identifier" host-addr
              | "authoritative" | "not" "authoritative"
              | "dhcp-profile" name "{" NL { [ dhcp-option ] NL } "}"
              | dhcp-option .
dhcp-option   = "option" "routers" host-addrs
              | "option" "domain-name-servers" host-addrs
              | "option" "ntp-servers" host-addrs
              | "option" "smtp-server" host-addrs
              | "option" "domain-name" name
              | "option" "domain-search" name-list
              | "option" "autoproxy-script" ( WORD | STRING )
              | "get-lease-hostnames" ( "true" | "false" )
              | "default-lease-time" duration
              | "max-lease-time" duration .

addr-list     = ADDRESS | "{" ADDRESS { [ "," ] ADDRESS } "}" .
name-list     = name    | "{" name    { [ "," ] name    } "}" .
net-list      = NETWORK | "{" NETWORK { [ "," ] NETWORK } "}" .

name          = WORD | STRING .
value         = WORD | STRING | "{" item { [ "," ] item } "}" .
duration      = NUMBER [ "s" | "m" | "h" | "d" | "w" ] .
SUFFIX        = "." OCTET { "." OCTET } .
MAC           = HEX HEX ":" HEX HEX ":" HEX HEX ":" HEX HEX ":" HEX HEX ":" HEX HEX .
```

The opening `{` of a block has to be on the same line as its keyword.
Options of `host`, `mx`, `srv` and `nameserver` can be given in any order,
but each only once.

## Statements

### Settings

A setting gives a value of the zone: the SOA contact, the default TTL and
the other SOA values. At top level it is the default for all zones; inside
a `zone` or `reverse` block it overrides that default for the block.

| Setting        | Default  | Meaning                                  |
|----------------|----------|------------------------------------------|
| `email`        | —        | SOA contact (`RNAME`); required          |
| `ttl`          | `3h`     | default TTL of the zone and its records  |
| `refresh`      | `2h`     | SOA refresh                              |
| `retry`        | `1h`     | SOA retry, must be less than `refresh`   |
| `expire`       | `2w`     | SOA expire                               |
| `negative-ttl` | `1h`     | SOA minimum (negative caching TTL)       |
| `serial`       | computed | fixed serial instead of `YYYYMMDDnn`     |

Settings at top level must come before the first `zone` or `reverse`
block. That way, reading the file from the top shows which defaults are in
effect. Inside a block, settings must come before the first record. Each
setting may be given once per scope.

The defaults in the table are the values of `zonefile-rs`
(`src/constants.rs`) written as durations: 10800 s = `3h`, 1209600 s = `2w`.

There is no `set` keyword: as in `httpd.conf(5)`, every line is a plain
statement, and the keyword alone tells a setting from a record. Switching
something off is written `no …`, see `ptr` and `no mx`.

Records without a priority get those of `zonefile-rs`: 0 for `mx`, and
priority 5 and weight 10 for `srv`.

### ptr, no ptr

```
ptr
no ptr
```

Whether hosts get PTR records; by default they do. At top level the
statement sets the default for all zones, inside a `zone` it overrides it
for that zone; at both places it follows the same ordering rule as the
settings. A host can override it again with `ptr` or `no ptr` (see
[host](#host)). `ptr` and `no ptr` are not allowed in a `reverse` block.

### nameserver, mx at top level

`nameserver` and `mx` at top level define the default NS and MX records
for every zone that has none of its own. Once a zone declares at least one
`nameserver` (or `mx`), the defaults of that type no longer apply to it.
A zone that should have no MX records at all, not even the top-level ones,
says `no mx`; `no mx` and `mx` in the same zone are an error. A
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
     [no ptr] [no inet] [no inet6] [mac MAC|{ MAC … }] [[no] dhcp]
     [dhcp-profile NAME]

host NAME ADDRESS|SUFFIX|{ … } [option …] {
	option …
	…
}
```

A host with many options may give them in a block instead, one or more
per line; the addresses stay in the first line. Each option may still be
given only once, in the line or in the block:

```
host tv .20 {
	alias www
	mac 00:00:5e:00:53:20
	dhcp dhcp-profile kids
}
```

Creates one A or AAAA record per address. A suffix expands to one address
per network of the zone (see [network](#network)); a full address is taken
as is. Both can be mixed in a list. `no inet` and `no inet6` drop the
addresses of that family, so `host printer .12 no inet6` is IPv4 only. `alias` names get the **same
addresses as additional A/AAAA records**, not CNAMEs, which matches
`zonefile-rs`. Only the host name itself gets a PTR record; aliases do
not. `no ptr` and `ptr` override the `ptr` or `no ptr` of the zone for
this host.

`mac` records the MAC addresses of the host; each may be used only once in
the configuration. On its own it changes no output. `dhcp` turns the host
on for [dhcp](#dhcp): it then gets a fixed address there, for which it
needs a `mac` and an IPv4 address in the network of a `dhcp` block.
`dhcp` and `no dhcp` in a zone set the default for its hosts, with the
ordering rule of the settings; without either, dhcp is off. In a zone with
`dhcp`, hosts without a `mac` are simply left out. `dhcp-profile` puts the
host into a profile of its dhcp block (see [dhcp](#dhcp)); in a zone it is
the default for its hosts.

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

### dhcp

```
dhcp 192.168.21.0/24 {
	range .100 .199
	option routers .1
	option domain-name-servers .1
	option domain-name example.com
	option domain-search { example.com apps.example.com }
	option ntp-servers .1
	default-lease-time 1d
	max-lease-time 7d
}
```

Describes one IPv4 subnet for the `dhcpd(8)` of OpenBSD, written with
`-t dhcpd` as a complete `dhcpd.conf(5)`. A `dhcp` block stands at top
level beside the zones, since several zones may share a network.
Addresses may be suffixes relative to its network.

The statements are named as in `dhcpd.conf(5)` and `dhcp-options(5)`,
so there is nothing to translate. Only the syntax is that of
zonefile.conf: suffixes for addresses, lists in braces, durations such as
`1d`, and no semicolon.

| Statement                     | Meaning                                       |
|-------------------------------|-----------------------------------------------|
| `range`                       | dynamic addresses; may be given several times |
| `server-identifier`           | address the clients use to reach this server; not in a profile |
| `authoritative`, `not authoritative` | whether dhcpd answers wrong requests with DHCPNAK; not in a profile |
| `option routers`              | default gateways; must lie within the network |
| `option domain-name-servers`  | resolvers of the clients                      |
| `option ntp-servers`          | NTP servers                                   |
| `option smtp-server`          | SMTP servers                                  |
| `option domain-name`          | domain of the clients                         |
| `option domain-search`        | search list of the clients                    |
| `option autoproxy-script`     | URL of the proxy configuration (WPAD)         |
| `get-lease-hostnames`         | `true` looks up a name for each dynamic address |
| `default-lease-time`          | lease time if the client asks for none        |
| `max-lease-time`              | longest lease time                            |

Other options of `dhcp-options(5)` are not supported yet; they can be
added with a type when needed, so that their values are checked.

#### The global dhcp block

```
dhcp {
	option domain-name-servers .1
	option domain-name example.com
	default-lease-time 1d
	max-lease-time 7d
}
```

A `dhcp` block without a network holds the defaults of all dhcp blocks.
There may be one. It takes the same statements as a dhcp block except
`range`. Its suffixes are resolved for each subnet, so `nameserver .1`
is the `.1` of every network; a full address is the same everywhere.

Options are inherited one by one: the global block, then the dhcp block,
then the profile of a host. Each level replaces an option it gives as a
whole; lists such as `nameserver { … }` are replaced, not merged. Checks
apply to the result for each subnet: `option routers` of the global block
must lie in every network, and `default-lease-time` must not be longer
than `max-lease-time`
whichever level gives them.

In `dhcpd.conf`, whatever the global block gives without suffixes stands
at the top level, and dhcpd passes it on to the subnets and groups. Lists
with suffixes differ per subnet, so they are resolved and written into
each subnet that does not give its own. A subnet holds only what its
block gives and these resolved values.

#### Profiles

```
dhcp 192.168.21.0/24 {
	option domain-name-servers .1
	dhcp-profile kids {
		option domain-name-servers .53
	}
}

zone example.com {
	host tv .20 mac 00:00:5e:00:53:20 dhcp dhcp-profile kids
}
```

A `dhcp-profile` block holds options for some hosts: the options of a
dhcp block, without `range`, `server-identifier` and `authoritative`.
Hosts and zones name it with `dhcp-profile` as well, so that one word
finds the definition and every use. A profile may stand in the global dhcp block, where it
serves every network and is resolved for each, or in a dhcp block, where
it serves that network. A host gets the profile of the dhcp block that
holds its address, or else the global one; a profile in a dhcp block
hides a global one of the same name for that network. In `dhcpd.conf`
each profile in use becomes a `group` inside the subnet, whose options
override those of the subnet for its hosts.

`dhcp-profile` rests like a `mac` while dhcp is off for the host. It is an
error if dhcp is on and the profile is defined neither in the host's dhcp
block nor in the global one, and a warning if dhcp is off and no block
defines the name at all, to catch typing errors early.

`zonefile-go -n` adds notes for valid configurations that may not be
intended. The normal run does not show them.

- A macro is defined but never used, as `pfctl` points out; it is often a
  typing error.
- A profile hides a global one of the same name.
- A profile is used by no host with dhcp.
- A dhcp block has neither a `range` nor a host with dhcp, so dhcpd
  answers no client there. That is right for a network dhcpd listens on
  but should not serve, and wrong if a `dhcp` was forgotten.
- An address in `option domain-name-servers` is not a nameserver of the
  zone named by `option domain-name`, checked for each subnet and each
  profile in use after inheritance. The NS names of the zone are resolved
  through the A records zonefile-go manages; if one of them lies outside,
  the check is left out for that zone. Clients would ask a resolver that
  need not know the zone, which is intended for a filtering or forwarding
  resolver, hence only a note.

Every host with `dhcp` on and a `mac` gets a host declaration in the
subnet that holds
its IPv4 address: `hardware ethernet`, `fixed-address` and `option
host-name` with the first label of its name. A host with several MAC
addresses gets one declaration per address, the further ones named
`name-2`, `name-3`, ….

Checks:

- dhcp networks are IPv4 and do not overlap; ranges lie within their
  network, start before they end and do not overlap; routers lie within
  the network; `default-lease-time` is not longer than `max-lease-time`.
- `dhcp` on a host needs a `mac`. A host with `dhcp` on and a `mac` needs
  an IPv4 address in the network of a `dhcp` block, and its fixed address
  must not lie in a dynamic range. A MAC address may be used only once,
  with or without `dhcp`.

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
- **Unique PTRs:** if two hosts would produce a PTR for the same address
  inside a reverse network, that is an error. `zonefile-rs` silently keeps
  one of them. Outside the reverse networks the same address may appear in
  several zones, since no PTR is generated for it.
- **Address suffixes:** see [network](#network).
- **Duplicates:** a `zone` must not be declared twice (names compare case
  insensitively), a host name must not appear twice in a zone, and the
  same name with the same address must not come out twice, e.g. once as a
  host and once as an alias of another host. Several hosts may share an
  alias with *different* addresses (round robin).
- **CNAMEs:** a CNAME must not share its name with any other record,
  including the zone apex.
- **Nameservers:** a nameserver whose name lies in a zone of the
  configuration needs an A or AAAA record in that zone, the most specific
  one, and must not be a CNAME (RFC 2181). All records of such a zone come
  from the configuration, so a missing address is an error. Nameservers
  outside the managed zones are not checked.
- **Out-of-zone data:** hosts, aliases, CNAMEs and SRV records must have
  names within their zone, e.g. `alias mail.h.example.net.` in `zone
  home.arpa` is an error. NSD would reject such a zone file. Targets may
  lie anywhere.

Warnings do not stop the zones from being written; notes, which point
out valid but possibly unintended configurations, are shown only by
`zonefile-go -n`. There is one warning about names: a
relative name that looks like a full one, because it repeats the zone name
or ends in a top-level domain (a two-letter country code, `com`, `net`,
`org`, `arpa`, `local`, `internal` and a few more). Without the trailing
dot the zone name is appended:

```
zones.conf:12: warning: "mail.home.arpa" ends in the top-level domain arpa
but is relative, so it becomes mail.home.arpa.h.example.net.; add a trailing
dot if you mean mail.home.arpa.
```

## Command line

Modelled on OpenBSD daemons:

```
zonefile-go [-nV] [-f file] [-o path] [-s serialfile] [-t unbound|nsd|dhcpd]
```

| Flag | Meaning                                                       |
|------|---------------------------------------------------------------|
| `-f` | configuration file (default `/etc/zonefile.conf`, on FreeBSD `/usr/local/etc/zonefile.conf`; `-` = stdin) |
| `-n` | check the configuration only, write nothing (like `pfctl -n`)  |
| `-o` | output file for unbound and dhcpd (default stdout), directory for nsd (default `nsd`) |
| `-s` | serial file (default `/var/db/zonefile-go.serial`)             |
| `-t` | output format, `unbound` (default), `nsd` or `dhcpd`           |
| `-V` | print the version                                              |

## Mapping from zonefile-rs

| zonefile-rs (YAML/TOML)            | zonefile.conf                         |
|------------------------------------|---------------------------------------|
| `defaults.email`                   | `email …`                             |
| `defaults.ttl` / `refresh` / …     | `ttl …` / `refresh …` / …             |
| `defaults.nrc-ttl`                 | `negative-ttl …`                      |
| `defaults.mx-prio`                 | `priority …` on each `mx`             |
| `defaults.srv-prio` / `srv-weight` | `priority …` / `weight …` on each `srv` |
| `defaults.with-ptr: false`         | top-level `no ptr`                    |
| `defaults.nameserver`              | top-level `nameserver …`              |
| `defaults.mx`                      | top-level `mx …`                      |
| `zone.<name>`                      | `zone <name> { … }`                   |
| `zone.<name>.with-ptr: false`      | `no ptr` in the zone                  |
| `hosts.<h>: ip`                    | `host <h> ip` or `host <h> .suffix`   |
| `hosts.<h>: {ip, alias, ttl}`      | `host <h> {…} alias {…} ttl …`        |
| `hosts.<h>.with-ptr: false`        | `host <h> … no ptr`                   |
| `cname.<n>: target`                | `cname <n> target`                    |
| `srv.<s>: {target, port, …}`       | `srv <s> target port … priority …`    |
| `reverse: [nets]`                  | `reverse { nets }`                    |
| `reverse.<net>: {options}`         | `reverse <net> { ttl … }`             |

## Open questions

1. **Migration aid:** a subcommand that converts existing TOML/YAML into
   this syntax?
2. **Defaults for `/etc`:** is `/etc/zonefile.conf` the right default path,
   or should the file have to be given with `-f`?
