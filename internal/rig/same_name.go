package rig

import "strings"

// SameName reports whether two rig names name the same rig: compared like the
// merge engine always has, case-insensitively and ignoring surrounding
// space. Empty names never match.
func SameName(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && strings.EqualFold(a, b)
}
