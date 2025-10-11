package aimux

import (
	"fmt"
	"strings"
)

// ValidateContextParams validates gen and mod are safe identifiers
func ValidateContextParams(cid, gen, mod string) error {
	if cid != "" && !isValidUUID(cid) {
		return fmt.Errorf("invalid cid: must be UUID format")
	}
	if !isValidIdentifier(gen) {
		return fmt.Errorf("invalid gen: must be alphanumeric/dash/dot/underscore, 1-64 chars")
	}
	if mod != "" && !isValidIdentifier(mod) {
		return fmt.Errorf("invalid mod: must be alphanumeric/dash/dot/underscore, 1-64 chars")
	}
	return nil
}

// NormalizeUUID converts a UUID string to lowercase
func NormalizeUUID(s string) string {
	return strings.ToLower(s)
}

// isValidUUID checks basic UUID format (accepts both uppercase and lowercase)
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// isValidIdentifier checks if string matches [-0-9A-Za-z._]{1,64}
func isValidIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}
