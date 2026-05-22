package tmuxctl

import "testing"

func TestParseRemoteLookup(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"clean target", "WHIP::work:0.1::END\n", "work:0.1"},
		{"profile noise before", "Welcome to box!\nLast login: ...\nWHIP::dev:2.0::END\n", "dev:2.0"},
		{"motd after", "WHIP::main:0.0::END\nbye\n", "main:0.0"},
		{"empty result", "WHIP::::END\n", ""},
		{"no sentinel", "tmux: server not running\n", ""},
		{"truncated sentinel", "WHIP::main:0.0\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRemoteLookup(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParsePanes(t *testing.T) {
	const out = "1234 work:0.0\n5678 work:1.2\nbroken-line\n91011 dev:0.0\n"
	m := parsePanes(out)
	if m[1234] != "work:0.0" {
		t.Errorf("1234: %q", m[1234])
	}
	if m[5678] != "work:1.2" {
		t.Errorf("5678: %q", m[5678])
	}
	if m[91011] != "dev:0.0" {
		t.Errorf("91011: %q", m[91011])
	}
	if _, ok := m[0]; ok {
		t.Errorf("expected no entry from broken line")
	}
}
