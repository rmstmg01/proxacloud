package vps

import (
	"testing"
)

func TestBlockedByAdministrativeSuspension(t *testing.T) {
	tests := []struct {
		name      string
		action    string
		suspended bool
		want      bool
	}{
		{name: "suspended VPS cannot start", action: "start", suspended: true, want: true},
		{name: "suspended VPS cannot restart", action: "restart", suspended: true, want: true},
		{name: "suspended VPS cannot shut down", action: "shutdown", suspended: true, want: true},
		{name: "unlocked VPS may start", action: "start", suspended: false, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := blockedByAdministrativeSuspension(test.action, test.suspended); got != test.want {
				t.Fatalf("blockedByAdministrativeSuspension(%q, %t) = %t, want %t", test.action, test.suspended, got, test.want)
			}
		})
	}
}

func TestAdministrativeSuspensionErrorIsStable(t *testing.T) {
	if got := ErrVPSAdministrativelySuspended.Error(); got != "VPS is administratively suspended" {
		t.Fatalf("unexpected public error: %q", got)
	}
}
