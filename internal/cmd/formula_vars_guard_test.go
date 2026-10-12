package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/formula"
)

const badVarsFormula = `formula = "bad-vars"
type = "workflow"
version = 1

[[steps]]
id = "one"
title = "Use {{missing_var}}"
description = "Uses {{declared}} and {{missing_var}}."

[vars]
[vars.declared]
description = "declared"
required = true
`

const goodVarsFormula = `formula = "good-vars"
type = "workflow"
version = 1

[[steps]]
id = "one"
title = "Use {{declared}}"
description = "Uses {{declared}}."

[vars]
[vars.declared]
description = "declared"
required = true
`

const orphanExtendsFormula = `formula = "orphan-child"
type = "workflow"
version = 1
extends = ["no-such-parent"]

[[steps]]
id = "one"
title = "Use {{parent_var}}"
description = "Relies on a var the missing parent would declare."
`

// setupFormulaVarsTown creates a town with rig "be" at the given readiness
// mode and the test formulas in the town formulas dir.
func setupFormulaVarsTown(t *testing.T, mode string) string {
	t.Helper()
	townRoot := t.TempDir()
	formulasDir := filepath.Join(townRoot, ".beads", "formulas")
	if err := os.MkdirAll(formulasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"bad-vars":     badVarsFormula,
		"good-vars":    goodVarsFormula,
		"orphan-child": orphanExtendsFormula,
	} {
		if err := os.WriteFile(filepath.Join(formulasDir, name+".formula.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	settingsDir := filepath.Join(townRoot, "be", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"type":"rig-settings","version":1,"readiness":"` + mode + `"}`
	if err := os.WriteFile(filepath.Join(settingsDir, "config.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	return townRoot
}

func TestValidateFormulaTemplateVars_UndefinedVar_Enforce(t *testing.T) {
	townRoot := setupFormulaVarsTown(t, "enforce")
	err := validateFormulaTemplateVars("bad-vars", townRoot, "be")
	if err == nil {
		t.Fatal("expected an error for an undefined template variable in enforce mode")
	}
	if !strings.Contains(err.Error(), "missing_var") || !strings.Contains(err.Error(), "bad-vars") {
		t.Errorf("error should name the formula and the variable, got: %v", err)
	}
}

func TestValidateFormulaTemplateVars_Warn_Continues(t *testing.T) {
	townRoot := setupFormulaVarsTown(t, "warn")
	var err error
	out := captureStdout(t, func() { err = validateFormulaTemplateVars("bad-vars", townRoot, "be") })
	if err != nil {
		t.Fatalf("warn mode must not fail: %v", err)
	}
	if !strings.Contains(out, "missing_var") {
		t.Errorf("warn mode should print the undefined variable, got: %q", out)
	}
}

func TestValidateFormulaTemplateVars_Off_NoCheck(t *testing.T) {
	townRoot := setupFormulaVarsTown(t, "off")
	out := captureStdout(t, func() {
		if err := validateFormulaTemplateVars("bad-vars", townRoot, "be"); err != nil {
			t.Fatalf("off mode must not fail: %v", err)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("off mode should print nothing, got: %q", out)
	}
}

func TestValidateFormulaTemplateVars_ValidFormula_Enforce(t *testing.T) {
	townRoot := setupFormulaVarsTown(t, "enforce")
	if err := validateFormulaTemplateVars("good-vars", townRoot, "be"); err != nil {
		t.Fatalf("valid formula rejected: %v", err)
	}
}

func TestValidateFormulaTemplateVars_UnresolvableParent_WarnsOnly(t *testing.T) {
	townRoot := setupFormulaVarsTown(t, "enforce")
	var err error
	out := captureStdout(t, func() { err = validateFormulaTemplateVars("orphan-child", townRoot, "be") })
	if err != nil {
		t.Fatalf("a formula the validator cannot resolve must not be rejected (cook decides): %v", err)
	}
	if !strings.Contains(out, "could not validate") {
		t.Errorf("expected a 'could not validate' warning, got: %q", out)
	}
}

func TestEmbeddedFormulas_PassTemplateValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := formula.ProvisionFormulas(dir); err != nil {
		t.Fatalf("ProvisionFormulas: %v", err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, ".beads", "formulas", "*.formula.toml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no embedded formulas provisioned (err=%v)", err)
	}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".formula.toml")
		f, err := loadFormulaForValidation(name, "", "")
		if err != nil {
			// Not parseable by gt's own parser (e.g. aspect formulas without
			// aspects); the guard only warns for these, so they cannot be
			// rejected. Only template-variable false positives fail this test.
			t.Logf("%s: skipped, not loadable for validation: %v", name, err)
			continue
		}
		if err := f.ValidateTemplateVariables(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInstantiateFormulaOnBead_UndefinedVar_Enforce_NoBdCalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	townRoot := setupFormulaVarsTown(t, "enforce")
	if err := beads.WriteRoutes(filepath.Join(townRoot, ".beads"), []beads.Route{{Prefix: "be-", Path: "be/mayor/rig"}}); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	writeBDStub(t, binDir, "#!/bin/sh\necho \"$*\" >> \""+logPath+"\"\necho '[]'\n", "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := InstantiateFormulaOnBead(context.Background(), "bad-vars", "be-123", "title", townRoot, townRoot, false, nil)
	if err == nil || !strings.Contains(err.Error(), "missing_var") {
		t.Fatalf("expected undefined-variable rejection, got %v", err)
	}
	if data, _ := os.ReadFile(logPath); len(data) > 0 {
		t.Errorf("no bd command may run before the rejection, got:\n%s", data)
	}
}
