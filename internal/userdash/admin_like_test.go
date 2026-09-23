package userdash

import "testing"

// TestLikeEscapeNeutralizesWildcardsAndBackslash pins the escaping contract for
// admin user search: the pattern is prefixed with the dialect-neutral escape
// character '!', and every LIKE metacharacter in the input ('%', '_'), the
// escape character itself ('!'), and a literal backslash are neutralized.
func TestLikeEscapeNeutralizesWildcardsAndBackslash(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "bob", want: "bob"},
		{name: "percent", input: "50%off", want: "50!%off"},
		{name: "underscore", input: "a_b", want: "a!_b"},
		{name: "escape char", input: "a!b", want: "a!!b"},
		{name: "backslash", input: `a\b`, want: `a\b`},
		{name: "all metacharacters", input: `%_!\`, want: `!%!_!!\`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := likeEscape(tc.input); got != tc.want {
				t.Fatalf("likeEscape(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
