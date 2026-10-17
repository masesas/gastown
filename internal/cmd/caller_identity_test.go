package cmd

import "testing"

// gt-pd1c.78: one classification of the caller for every coordinator guard
// (pre-push, nuke --force, landing verdict).
func TestGTCallerFrom(t *testing.T) {
	cases := []struct {
		name   string
		env    []string
		role   Role
		exempt bool
	}{
		{"human", nil, "", true},
		{"crew compound", []string{"GT_ROLE", "gastown/crew/max"}, RoleCrew, true},
		{"crew bare", []string{"GT_ROLE", "crew"}, RoleCrew, true},
		{"crew via GT_CREW only", []string{"GT_CREW", "max"}, RoleCrew, true},
		{"crew via BD_ACTOR only", []string{"BD_ACTOR", "gastown/crew/max"}, RoleCrew, true},
		{"witness", []string{"GT_ROLE", "gastown/witness"}, RoleWitness, false},
		{"refinery", []string{"GT_ROLE", "gastown/refinery"}, RoleRefinery, false},
		{"mayor", []string{"GT_ROLE", "mayor"}, RoleMayor, false},
		{"mayor via BD_ACTOR only", []string{"BD_ACTOR", "mayor"}, RoleMayor, false},
		{"deacon", []string{"GT_ROLE", "deacon"}, RoleDeacon, false},
		{"boot", []string{"GT_ROLE", "deacon/boot"}, RoleBoot, false},
		{"dog", []string{"GT_ROLE", "dog"}, RoleDog, false},
		{"polecat", []string{"GT_ROLE", "gastown/polecats/nux"}, RolePolecat, false},
		{"polecat via GT_POLECAT only", []string{"GT_POLECAT", "nux"}, RolePolecat, false},
		{"polecat claiming crew via GT_ROLE", []string{"GT_ROLE", "gastown/crew/max", "GT_POLECAT", "nux"}, RolePolecat, false},
		{"polecat actor claiming crew", []string{"GT_ROLE", "gastown/crew/max", "BD_ACTOR", "gastown/polecats/nux"}, RolePolecat, false},
		{"unknown role is an agent", []string{"GT_ROLE", "something-else"}, Role("something-else"), false},
		{"overseer actor is human", []string{"BD_ACTOR", "overseer"}, roleOverseer, true},
		{"daemon actor is an agent", []string{"BD_ACTOR", "daemon"}, Role("daemon"), false},
		{"whitespace only is human", []string{"GT_ROLE", "  "}, "", true},
	}
	for _, c := range cases {
		got := gtCallerFrom(prePushEnv(c.env...))
		if got.role != c.role || got.exempt() != c.exempt {
			t.Errorf("%s: got role=%q exempt=%v, want role=%q exempt=%v", c.name, got.role, got.exempt(), c.role, c.exempt)
		}
		if (got.via == "") != (len(c.env) == 0 || c.name == "whitespace only is human") {
			t.Errorf("%s: via=%q", c.name, got.via)
		}
	}
}
