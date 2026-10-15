// Package sandboxpath validates paths relative to a sandbox root.
package sandboxpath

import (
	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid sandbox path")
var ErrEscape = errors.New("path escapes sandbox")

// Validate rejects paths that traverse above the sandbox root.
func Validate(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
		return ErrInvalid
	}
	depth := 0
	for _, segment := range strings.Split(value, "/") {
		switch segment {
		case "", ".":
		case "..":
			if depth == 0 {
				return ErrEscape
			}
			depth--
		default:
			depth++
		}
	}
	return nil
}
