package mailcloak

import "testing"

func TestDomainFromEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
		ok    bool
	}{
		{name: "lowercase domain", email: "Alice@Example.COM", want: "example.com", ok: true},
		{name: "invalid no at", email: "alice.example.com", ok: false},
		{name: "invalid empty local", email: "@example.com", ok: false},
		{name: "invalid empty domain", email: "alice@", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := domainFromEmail(tc.email)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("domainFromEmail(%q) = (%q, %v), want (%q, %v)", tc.email, got, ok, tc.want, tc.ok)
			}
		})
	}
}
