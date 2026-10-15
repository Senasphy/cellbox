package sandboxpath

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, value := range []string{"/tmp/file", "tmp/file", "a/../file", "/tmp/../etc/passwd", "."} {
		if err := Validate(value); err != nil {
			t.Errorf("Validate(%q): %v", value, err)
		}
	}
	for _, value := range []string{"../file", "../../etc/passwd", "/../file", "/../../etc/passwd", "/a/../../file"} {
		if err := Validate(value); !errors.Is(err, ErrEscape) {
			t.Errorf("Validate(%q) error=%v, want ErrEscape", value, err)
		}
	}
	for _, value := range []string{"", "  ", "/tmp/\x00file"} {
		if err := Validate(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("Validate(%q) error=%v, want ErrInvalid", value, err)
		}
	}
}
