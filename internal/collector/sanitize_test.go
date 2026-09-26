package collector

import "testing"

func TestSanitizeLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"IPTV #1/THOTH/playlist.php - XTRM/Service01", "IPTV #1/THOTH/playlist.php - XTRM/Service01"},
		{"IPTV/http://xt.example.com:8080/USER/PASS/123.ts", "IPTV/<url>"},
		{"IPTV/https://cdn.example.net/live/ch1.m3u8?token=s3cr3t&x=1", "IPTV/<url>"},
		{"IPTV/http://user:pw@198.51.100.9:9981/stream/channel/abc", "IPTV/<url>"},
		{"a udp://239.0.0.1:1234 b", "a <url> b"},
		{"x http://h/p?q=1 and rtsp://h2/p2", "x <url> and <url>"},
		{"IPTV #1/THOTH/http://u:p@iptv.example.net:8080/live?token=t Service01", "IPTV #1/THOTH/<url> Service01"},
		{"svc/git+ssh://host/x", "svc/<url>"},
		{"://nohost", "<url>"},
		{"DVR: Secret Programme Title", "DVR"},
		{"HTTP", "HTTP"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeLabel(c.in); got != c.want {
			t.Errorf("sanitizeLabel(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
