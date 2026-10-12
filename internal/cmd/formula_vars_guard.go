package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/formula"
	"github.com/steveyegge/gastown/internal/style"
)

// validateFormulaTemplateVars checks, before a formula is cooked, that every
// {{var}} it uses is declared in its [vars]/[inputs]. The rig's readiness
// mode decides the outcome: off skips the check, warn prints, enforce returns
// an error so sling stops before any side effect. A formula the validator
// cannot load or resolve is only warned about; cook remains the authority.
func validateFormulaTemplateVars(formulaName, townRoot, rigName string) error {
	mode := readinessModeForRig(townRoot, rigName)
	if mode == config.ReadinessOff || formulaName == "" {
		return nil
	}

	f, err := loadFormulaForValidation(formulaName, townRoot, rigName)
	if err != nil {
		fmt.Printf("%s could not validate template variables of formula %s: %v\n", style.Dim.Render("Warning:"), formulaName, err)
		return nil
	}
	verr := f.ValidateTemplateVariables()
	if verr == nil {
		return nil
	}
	if mode == config.ReadinessEnforce {
		return fmt.Errorf("formula %s: %w\nFix the formula, or set readiness to warn for rig %q", formulaName, verr, rigName)
	}
	fmt.Printf("%s formula %s: %v\n", style.Warning.Render("⚠"), formulaName, verr)
	return nil
}

// loadFormulaForValidation loads formulaName the way cook finds it (rig, town,
// embedded; with the "mol-" prefix fallback) and resolves extends so variables
// declared by parents count as declared. Only extends inherits [vars], so
// other formulas (including aspect/compose ones) are validated as written.
func loadFormulaForValidation(formulaName, townRoot, rigName string) (*formula.Formula, error) {
	content, err := formula.ResolveFormulaContent(formulaName, townRoot, rigName)
	if err != nil && !strings.HasPrefix(formulaName, "mol-") {
		content, err = formula.ResolveFormulaContent("mol-"+formulaName, townRoot, rigName)
	}
	if err != nil {
		return nil, err
	}
	f, err := formula.Parse(content)
	if err != nil {
		return nil, err
	}
	if len(f.Extends) == 0 {
		return f, nil
	}

	var searchPaths []string
	if townRoot != "" {
		if rigName != "" {
			searchPaths = append(searchPaths, filepath.Join(townRoot, rigName, ".beads", "formulas"))
		}
		searchPaths = append(searchPaths, filepath.Join(townRoot, ".beads", "formulas"))
	}
	return formula.Resolve(f, searchPaths)
}
