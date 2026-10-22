package depcheck

import "testing"

// TestGOFlags covers the flag merge: inherited flags other than a -mod
// are kept, an inherited -mod is replaced by -mod=readonly, and the
// readonly flag is appended once at the end.
func TestGOFlags(t *testing.T) {
	cases := []struct {
		name      string
		inherited string
		want      string
	}{
		{
			name:      "no inherited flags",
			inherited: "",
			want:      "-mod=readonly",
		},
		{
			name:      "an inherited mod is dropped",
			inherited: "-mod=vendor",
			want:      "-mod=readonly",
		},
		{
			name:      "other inherited flags are kept",
			inherited: "-tags=poo_slow -count=1",
			want:      "-tags=poo_slow -count=1 -mod=readonly",
		},
		{
			name:      "a mixed line keeps the tags and drops the mod",
			inherited: "-tags=x -mod=vendor -v",
			want:      "-tags=x -v -mod=readonly",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := goflags(c.inherited); got != c.want {
				t.Errorf("goflags(%q) is %q, want %q", c.inherited, got, c.want)
			}
		})
	}
}
