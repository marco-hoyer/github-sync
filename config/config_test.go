package config

import "testing"

func TestAllowsOrg(t *testing.T) {
	tests := []struct {
		name string
		orgs []string
		org  string
		want bool
	}{
		{"no filter allows all", nil, "anything", true},
		{"listed org allowed", []string{"acme", "foo"}, "foo", true},
		{"match is case-insensitive", []string{"Acme"}, "acme", true},
		{"unlisted org rejected", []string{"acme"}, "other", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := GitHubInstance{Orgs: tt.orgs}
			if got := inst.AllowsOrg(tt.org); got != tt.want {
				t.Errorf("AllowsOrg(%q) = %v, want %v", tt.org, got, tt.want)
			}
		})
	}
}
