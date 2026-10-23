package protect_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sdougbrown/poo/protect"
)

// masker is the shape a sandboxing executor declares for the paths it
// masks.
type masker interface {
	Expand(root string) ([]string, error)
}

// The set is what an executor masks with.
var _ masker = (*protect.Set)(nil)

// tempDir returns a temporary directory with its symlinks resolved.
//
// The tests match resolved paths, and a temporary directory may sit
// behind a symlink.
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolving %s: %v", dir, err)
	}
	return resolved
}

// writeFile writes a file and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// TestAnEmptySetMatchesNothing covers the default: a run with no
// protected paths configured protects nothing.
func TestAnEmptySetMatchesNothing(t *testing.T) {
	set, err := protect.Compile(nil)
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("the set holds %d globs, want none", set.Len())
	}
	if set.Match("/etc/passwd") {
		t.Error("the empty set matched a path")
	}
	found, err := set.Expand(tempDir(t))
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("the empty set expanded to %v, want nothing", found)
	}
}

// TestTheHomeDirectoryStandsForTheUser covers the "~" in a glob.
func TestTheHomeDirectoryStandsForTheUser(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	set, err := protect.Compile([]string{"~/.ssh/**"})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := filepath.Join(home, ".ssh", "id_rsa")
	if !set.Match(want) {
		t.Errorf("the set did not match %s", want)
	}
	if set.Match(filepath.Join(home, ".config", "poo", "config.yaml")) {
		t.Error("the set matched a path outside the glob")
	}
	// The glob is expanded, so a caller can see what it protects.
	if got := set.Globs(); len(got) != 1 || got[0] != filepath.Join(home, ".ssh", "**") {
		t.Errorf("the globs are %v, want the expanded glob", got)
	}
}

// TestADoubleStarCrossesTheSeparators covers the "**" of a glob.
func TestADoubleStarCrossesTheSeparators(t *testing.T) {
	set, err := protect.Compile([]string{"/srv/**/secrets.yaml"})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	for _, path := range []string{
		"/srv/secrets.yaml",
		"/srv/app/secrets.yaml",
		"/srv/app/deep/secrets.yaml",
	} {
		if !set.Match(path) {
			t.Errorf("the set did not match %s", path)
		}
	}
	for _, path := range []string{
		"/srv/app/secrets.yml",
		"/other/app/secrets.yaml",
		"/srv/app/deep/other.yaml",
	} {
		if set.Match(path) {
			t.Errorf("the set matched %s", path)
		}
	}
}

// TestASingleStarStaysInsideOneSegment covers the "*" of a glob: it does
// not cross a separator.
func TestASingleStarStaysInsideOneSegment(t *testing.T) {
	set, err := protect.Compile([]string{"/srv/*.env"})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if !set.Match("/srv/app.env") {
		t.Error("the set did not match a file in the directory")
	}
	if set.Match("/srv/app/app.env") {
		t.Error("the set matched a file in a subdirectory")
	}
}

// TestASymlinkToAProtectedPathIsProtected covers the resolution of a
// path: the link is protected because its target is.
func TestASymlinkToAProtectedPathIsProtected(t *testing.T) {
	dir := tempDir(t)
	target := writeFile(t, dir, "secret.txt", "the secret")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(dir, "secret.txt")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if !set.Match(link) {
		t.Error("the set did not match the symlink to a protected file")
	}
	if !set.Match(target) {
		t.Error("the set did not match the protected file")
	}
	if set.Match(writeFile(t, dir, "plain.txt", "plain")) {
		t.Error("the set matched a file that is not protected")
	}
}

// TestASymlinkToAProtectedDirectoryIsProtected covers a link to a
// directory: the path inside it is protected.
func TestASymlinkToAProtectedDirectoryIsProtected(t *testing.T) {
	dir := tempDir(t)
	secretDir := filepath.Join(dir, "private")
	writeFile(t, secretDir, "key.pem", "the key")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(secretDir, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(dir, "private", "**")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if !set.Match(filepath.Join(link, "key.pem")) {
		t.Error("the set did not match the file behind the symlink")
	}
}

// TestExpandReturnsTheProtectedPathsUnderTheRoot covers the expansion:
// the paths under the root that are protected, sorted.
func TestExpandReturnsTheProtectedPathsUnderTheRoot(t *testing.T) {
	dir := tempDir(t)
	writeFile(t, dir, "keep.txt", "keep")
	writeFile(t, dir, "private/one.txt", "one")
	writeFile(t, dir, "private/two.txt", "two")
	writeFile(t, dir, "other/three.txt", "three")

	set, err := protect.Compile([]string{filepath.Join(dir, "private", "**")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{
		filepath.Join(dir, "private"),
		filepath.Join(dir, "private", "one.txt"),
		filepath.Join(dir, "private", "two.txt"),
	}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
}

// TestExpandDoesNotFollowSymlinks covers the walk: a symlinked directory
// is not entered, so the files behind it are not protected paths.
func TestExpandDoesNotFollowSymlinks(t *testing.T) {
	dir := tempDir(t)
	writeFile(t, dir, "private/one.txt", "one")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "private"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{"/**/one.txt"})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	for _, path := range found {
		if path == filepath.Join(dir, "link", "one.txt") {
			t.Errorf("the expansion followed the symlink: %v", found)
		}
	}
}

// TestExpandNamesAMatchUnderTheRootsOwnSpelling covers a glob written
// through a symlinked root: the root is walked through the link, and the
// match comes back under the name the caller mounts it by.
func TestExpandNamesAMatchUnderTheRootsOwnSpelling(t *testing.T) {
	dir := tempDir(t)
	real := filepath.Join(dir, "real")
	private := writeFile(t, real, "private.txt", "the private text")
	writeFile(t, real, "keep.txt", "keep")
	link := filepath.Join(dir, "linked-root")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	spelled := filepath.Join(link, "private.txt")
	set, err := protect.Compile([]string{spelled})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(link)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{spelled}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
	found, err = set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding %s: %v", dir, err)
	}
	want = []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion under %s is %v, want %v", dir, found, want)
	}

	// A glob that names the root's link protects the root the link
	// reaches, and the topmost match is the root itself.
	set, err = protect.Compile([]string{link})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err = set.Expand(real)
	if err != nil {
		t.Fatalf("expanding %s: %v", real, err)
	}
	want = []string{real}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion under %s is %v, want %v", real, found, want)
	}
}

// TestExpandWalksThroughASymlinkedPrefix covers a glob whose fixed prefix
// is a symlink: the directory it reaches is walked, so a wildcard under a
// link finds what the link holds.
func TestExpandWalksThroughASymlinkedPrefix(t *testing.T) {
	dir := tempDir(t)
	real := filepath.Join(dir, "real")
	private := writeFile(t, real, "private.txt", "the private text")
	writeFile(t, real, "keep.txt", "keep")
	link := filepath.Join(dir, "linked-root")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(link, "private.*")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
}

// TestExpandResolvesAnIntermediateSymlink covers a glob with a symlinked
// directory in the middle of its prefix: the path behind the link is the
// one a sandbox can mask.
func TestExpandResolvesAnIntermediateSymlink(t *testing.T) {
	dir := tempDir(t)
	private := writeFile(t, dir, "nested/deeper/private.txt", "the private text")
	link := filepath.Join(dir, "linked-dir")
	if err := os.Symlink(filepath.Join(dir, "nested", "deeper"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(link, "private.txt")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
}

// TestExpandNamesTheTargetOfAProtectedFileSymlink covers a glob that names
// a link: the link is not a mount destination, so the path it reaches is
// the one to mask, and a target outside the root is not in the sandbox to
// mask at all.
func TestExpandNamesTheTargetOfAProtectedFileSymlink(t *testing.T) {
	dir := tempDir(t)
	private := writeFile(t, dir, "private.txt", "the private text")
	inside := filepath.Join(dir, "inside.txt")
	if err := os.Symlink(private, inside); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	outside := writeFile(t, tempDir(t), "secret.txt", "the secret")
	beyond := filepath.Join(dir, "beyond.txt")
	if err := os.Symlink(outside, beyond); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	set, err := protect.Compile([]string{inside})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}

	set, err = protect.Compile([]string{beyond})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err = set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("the expansion names the link or its target outside the root: %v", found)
	}
}

// TestExpandMasksTheTargetOfAProtectedFileSymlink covers a wildcard that
// matches a link: the match is masked at the path it reaches, because a
// mount cannot land on a link.
func TestExpandMasksTheTargetOfAProtectedFileSymlink(t *testing.T) {
	dir := tempDir(t)
	private := writeFile(t, dir, "private.txt", "the private text")
	writeFile(t, dir, "keep.md", "keep")
	if err := os.Symlink(private, filepath.Join(dir, "alias.txt")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	set, err := protect.Compile([]string{filepath.Join(dir, "*.txt")})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
}

// TestARelativeGlobIsRefused covers the glob that would protect nothing.
func TestARelativeGlobIsRefused(t *testing.T) {
	if _, err := protect.Compile([]string{"secrets/**"}); err == nil {
		t.Error("a relative glob was accepted")
	}
}

// TestAMalformedGlobIsRefused covers the glob that cannot be matched.
func TestAMalformedGlobIsRefused(t *testing.T) {
	if _, err := protect.Compile([]string{"/srv/[a-"}); err == nil {
		t.Error("a malformed glob was accepted")
	}
}

// TestAnEmptyGlobIsSkipped covers the empty entry in a list of globs.
func TestAnEmptyGlobIsSkipped(t *testing.T) {
	set, err := protect.Compile([]string{"", "   ", "/srv/**"})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if set.Len() != 1 {
		t.Errorf("the set holds %d globs, want one", set.Len())
	}
}

// TestExpandReturnsALiteralProtectedFile covers a glob that names one
// file: the fixed prefix is the file itself, and the expansion still has
// to name it so a sandbox can hide it.
func TestExpandReturnsALiteralProtectedFile(t *testing.T) {
	dir := tempDir(t)
	private := writeFile(t, dir, "private.txt", "the private text")
	writeFile(t, dir, "keep.txt", "keep")

	set, err := protect.Compile([]string{private})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	found, err := set.Expand(dir)
	if err != nil {
		t.Fatalf("expanding: %v", err)
	}
	want := []string{private}
	if !slices.Equal(found, want) {
		t.Errorf("the expansion is %v, want %v", found, want)
	}
}
