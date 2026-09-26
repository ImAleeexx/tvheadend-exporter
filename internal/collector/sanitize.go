package collector

import "strings"

// urlPlaceholder replaces every URL found in a free-text label value.
const urlPlaceholder = "<url>"

// sanitizeLabel strips secrets from free-text API values (subscription
// service/title, input stream).
// URLs are never exported: every "scheme://..." span collapses to "<url>",
// keeping no scheme, host, port, userinfo, path or query. The span runs from
// the scheme's first character to the next whitespace, since IPTV mux URLs
// sit inside "net/mux/service" names and their path cannot be told apart
// from what follows. DVR subscription titles ("DVR: <programme>") collapse
// to "DVR".
func sanitizeLabel(v string) string {
	if strings.HasPrefix(v, "DVR:") {
		return "DVR"
	}
	var b strings.Builder
	for {
		i := strings.Index(v, "://")
		if i < 0 {
			b.WriteString(v)
			return b.String()
		}
		start := i
		for start > 0 && isSchemeChar(v[start-1]) {
			start--
		}
		b.WriteString(v[:start])
		b.WriteString(urlPlaceholder)
		rest := v[i+3:]
		end := strings.IndexAny(rest, " \t\r\n")
		if end < 0 {
			end = len(rest)
		}
		v = rest[end:]
	}
}

// isSchemeChar reports whether c may appear in a URL scheme (RFC 3986).
func isSchemeChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'
}
