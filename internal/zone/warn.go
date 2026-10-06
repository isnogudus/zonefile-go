package zone

import "strings"

// classicTLDs are the top-level domains, besides the two-letter country
// codes, that a relative name is unlikely to end in on purpose. The list is
// short on purpose: the full IANA list contains words such as "host" or
// "services" that are common as ordinary labels.
var classicTLDs = map[string]bool{
	"arpa": true, "biz": true, "com": true, "edu": true, "gov": true,
	"info": true, "int": true, "internal": true, "invalid": true,
	"local": true, "localhost": true, "mil": true, "name": true,
	"net": true, "org": true, "test": true,
}

// looksAbsolute reports why a relative name probably was meant to be
// absolute: it repeats the zone name, or ends in a top-level domain. It
// returns "" if the name looks like an intended relative name.
func looksAbsolute(name, origin string) string {
	if name == "@" || strings.HasSuffix(name, ".") || !strings.Contains(name, ".") {
		return ""
	}
	lower := strings.ToLower(name)
	zone := strings.ToLower(strings.TrimSuffix(origin, "."))
	if lower == zone || strings.HasSuffix(lower, "."+zone) {
		return "repeats the zone name"
	}
	tld := lower[strings.LastIndexByte(lower, '.')+1:]
	if classicTLDs[tld] || len(tld) == 2 && isLetters(tld) {
		return "ends in the top-level domain " + tld
	}
	return ""
}

func isLetters(s string) bool {
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
