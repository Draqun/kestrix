package domain

import "testing"

func TestRole_AtLeast(t *testing.T) {
	tests := []struct {
		name string
		have Role
		want Role
		ok   bool
	}{
		{"viewer meets viewer", RoleViewer, RoleViewer, true},
		{"viewer below operator", RoleViewer, RoleOperator, false},
		{"viewer below admin", RoleViewer, RoleAdmin, false},
		{"operator meets viewer", RoleOperator, RoleViewer, true},
		{"operator meets operator", RoleOperator, RoleOperator, true},
		{"operator below admin", RoleOperator, RoleAdmin, false},
		{"admin meets viewer", RoleAdmin, RoleViewer, true},
		{"admin meets operator", RoleAdmin, RoleOperator, true},
		{"admin meets admin", RoleAdmin, RoleAdmin, true},
		{"unknown role meets nothing", Role("bogus"), RoleViewer, false},
		{"zero-value role meets nothing", Role(""), RoleViewer, false},
		{"known role does not meet unknown", RoleViewer, Role("bogus"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.have.AtLeast(tt.want); got != tt.ok {
				t.Errorf("%s.AtLeast(%s) = %v, want %v", tt.have, tt.want, got, tt.ok)
			}
		})
	}
}

func TestRole_Valid(t *testing.T) {
	for _, r := range []Role{RoleViewer, RoleOperator, RoleAdmin} {
		if !r.Valid() {
			t.Errorf("%s.Valid() = false, want true", r)
		}
	}
	for _, r := range []Role{Role("bogus"), Role("")} {
		if r.Valid() {
			t.Errorf("%s.Valid() = true, want false", r)
		}
	}
}

func TestHigherRole(t *testing.T) {
	tests := []struct {
		a, b, want Role
	}{
		{RoleViewer, RoleOperator, RoleOperator},
		{RoleOperator, RoleViewer, RoleOperator},
		{RoleAdmin, RoleViewer, RoleAdmin},
		{RoleOperator, RoleOperator, RoleOperator},
		{Role("bogus"), RoleViewer, RoleViewer},
		{RoleViewer, Role("bogus"), RoleViewer},
	}
	for _, tt := range tests {
		if got := HigherRole(tt.a, tt.b); got != tt.want {
			t.Errorf("HigherRole(%s, %s) = %s, want %s", tt.a, tt.b, got, tt.want)
		}
	}
}
