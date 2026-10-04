package zone

import (
	"fmt"
	"net/netip"
	"strings"
)

// absolute resolves name relative to origin, which is absolute.
func absolute(name, origin string) string {
	switch {
	case strings.HasSuffix(name, "."):
		return name
	case name == "@":
		return origin
	}
	return name + "." + origin
}

// validName checks an absolute DNS name: at most 253 characters, labels of
// 1 to 63 letters, digits, hyphens and underscores, no hyphen at either end
// of a label, and "*" only as the whole leftmost label.
func validName(name string) error {
	trimmed := strings.TrimSuffix(name, ".")
	if trimmed == "" {
		return fmt.Errorf("invalid name %q", name)
	}
	if len(trimmed) > 253 {
		return fmt.Errorf("name %q is longer than 253 characters", name)
	}
	for i, label := range strings.Split(trimmed, ".") {
		switch {
		case label == "":
			return fmt.Errorf("name %q has an empty label", name)
		case len(label) > 63:
			return fmt.Errorf("label %q in %q is longer than 63 characters", label, name)
		case label == "*":
			if i != 0 {
				return fmt.Errorf("wildcard in %q must be the leftmost label", name)
			}
			continue
		case label[0] == '-' || label[len(label)-1] == '-':
			return fmt.Errorf("label %q in %q starts or ends with a hyphen", label, name)
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("label %q in %q contains %q", label, name, c)
			}
		}
	}
	return nil
}

// rname converts an e-mail address into the RNAME of an SOA record:
// john.doe@example.com becomes john\.doe.example.com.
func rname(email string) string {
	local, domain, _ := strings.Cut(email, "@")
	return strings.ReplaceAll(local, ".", `\.`) + "." + strings.TrimSuffix(domain, ".") + "."
}

// suffixString formats an address suffix as written, e.g. .21.37.
func suffixString(suffix []byte) string {
	var b strings.Builder
	for _, o := range suffix {
		fmt.Fprintf(&b, ".%d", o)
	}
	return b.String()
}

// applySuffix places the suffix, read as an unsigned integer, into the host
// part of n: .37 in 192.168.21.0/24 is 192.168.21.37, in fd00::/64 it is
// fd00::25.
func applySuffix(n netip.Prefix, suffix []byte) (netip.Addr, error) {
	var value uint64
	for _, o := range suffix {
		value = value<<8 | uint64(o)
	}
	hostBits := n.Addr().BitLen() - n.Bits()
	if hostBits < 64 && value >= 1<<hostBits {
		return netip.Addr{}, fmt.Errorf("suffix %s does not fit into the host part of %s", suffixString(suffix), n)
	}
	if n.Addr().Is4() && hostBits >= 2 && (value == 0 || value == 1<<hostBits-1) {
		return netip.Addr{}, fmt.Errorf("suffix %s is the network or broadcast address of %s", suffixString(suffix), n)
	}
	b := n.Addr().AsSlice()
	for i := len(b) - 1; value > 0; i-- {
		b[i] |= byte(value)
		value >>= 8
	}
	addr, _ := netip.AddrFromSlice(b)
	return addr, nil
}

// reverseAligned reports whether n ends on a label boundary of the reverse
// tree: octets for IPv4, nibbles for IPv6.
func reverseAligned(n netip.Prefix) bool {
	if n.Addr().Is4() {
		return n.Bits()%8 == 0
	}
	return n.Bits()%4 == 0
}

// reverseLabels returns the labels of the reverse name of addr, most
// significant first, so that a prefix of them names a network.
func reverseLabels(addr netip.Addr) []string {
	b := addr.AsSlice()
	var labels []string
	if addr.Is4() {
		for _, o := range b {
			labels = append(labels, fmt.Sprint(o))
		}
		return labels
	}
	for _, o := range b {
		labels = append(labels, fmt.Sprintf("%x", o>>4), fmt.Sprintf("%x", o&0xf))
	}
	return labels
}

func reverseName(labels []string, v4 bool) string {
	rev := make([]string, 0, len(labels)+1)
	for i := len(labels) - 1; i >= 0; i-- {
		rev = append(rev, labels[i])
	}
	if v4 {
		rev = append(rev, "in-addr.arpa.")
	} else {
		rev = append(rev, "ip6.arpa.")
	}
	return strings.Join(rev, ".")
}

// reverseZoneName returns the name of the reverse zone for n, which must
// be aligned: 192.168.0.0/16 is 168.192.in-addr.arpa.
func reverseZoneName(n netip.Prefix) string {
	per := 8
	if n.Addr().Is6() {
		per = 4
	}
	return reverseName(reverseLabels(n.Addr())[:n.Bits()/per], n.Addr().Is4())
}

// reverseAddrName returns the full reverse name of addr.
func reverseAddrName(addr netip.Addr) string {
	return reverseName(reverseLabels(addr), addr.Is4())
}
