package protect_test

import (
	"testing"

	"github.com/sdougbrown/poo/tool/protect"
)

// TestATrailingSlashStillProtects covers the way an operator writes a
// directory: "/etc/" protects /etc and what it holds.
func TestATrailingSlashStillProtects(t *testing.T) {
	for _, glob := range []string{"/etc/", "/etc", "/etc/**"} {
		set, err := protect.Compile([]string{glob})
		if err != nil {
			t.Fatalf("compiling %q: %v", glob, err)
		}
		for _, target := range []string{"/etc", "/etc/passwd"} {
			if !set.Match(target) {
				t.Errorf("the glob %q does not protect %s (%v)", glob, target, set.Globs())
			}
		}
	}
}
