package exporter

// orb_semver_test.go covers uncovered branches in semverLess and
// parseSemverTuple. These helpers live in orb.go and are in package exporter
// (not exporter_test) so we can call them directly.

import (
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// parseSemverTuple
// ---------------------------------------------------------------------------

// TestParseSemverTuple_Valid verifies the happy path.
func TestParseSemverTuple_Valid(t *testing.T) {
	cases := []struct {
		in        string
		wantMajor int
		wantMinor int
		wantPatch int
	}{
		{"1.2.3", 1, 2, 3},
		{"0.0.0", 0, 0, 0},
		{"10.20.30", 10, 20, 30},
		{"v1.2.3", 1, 2, 3},           // leading v
		{"1.2.3-beta.1", 1, 2, 3},     // pre-release stripped
		{"1.2.3+build.456", 1, 2, 3},  // build metadata stripped
		{"1.2.3-alpha+meta", 1, 2, 3}, // both suffix types
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseSemverTuple(tc.in)
			if !ok {
				t.Fatalf("parseSemverTuple(%q) ok=false, want true", tc.in)
			}
			if got[0] != tc.wantMajor || got[1] != tc.wantMinor || got[2] != tc.wantPatch {
				t.Errorf("parseSemverTuple(%q) = %v, want [%d %d %d]",
					tc.in, got, tc.wantMajor, tc.wantMinor, tc.wantPatch)
			}
		})
	}
}

// TestParseSemverTuple_TwoPartVersion verifies that a version with only
// major.minor (no patch) returns false — the current implementation requires
// all three components.
func TestParseSemverTuple_TwoPartVersion(t *testing.T) {
	_, ok := parseSemverTuple("1.2")
	if ok {
		t.Errorf("parseSemverTuple(%q) ok=true, want false for 2-part version", "1.2")
	}
}

// TestParseSemverTuple_Empty verifies that an empty string returns false.
func TestParseSemverTuple_Empty(t *testing.T) {
	_, ok := parseSemverTuple("")
	if ok {
		t.Errorf("parseSemverTuple(%q) ok=true, want false for empty string", "")
	}
}

// TestParseSemverTuple_NonNumericComponent verifies that a non-numeric
// component (e.g. "1.x.3") returns false.
func TestParseSemverTuple_NonNumericComponent(t *testing.T) {
	_, ok := parseSemverTuple("1.x.3")
	if ok {
		t.Errorf("parseSemverTuple(%q) ok=true, want false for non-numeric component", "1.x.3")
	}
}

// TestParseSemverTuple_OnlyMajor verifies that a single integer (no dots)
// returns false.
func TestParseSemverTuple_OnlyMajor(t *testing.T) {
	_, ok := parseSemverTuple("42")
	if ok {
		t.Errorf("parseSemverTuple(%q) ok=true, want false for single-component", "42")
	}
}

// ---------------------------------------------------------------------------
// semverLess
// ---------------------------------------------------------------------------

// TestSemverLess_Valid verifies strict ordering with well-formed versions.
func TestSemverLess_Valid(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "2.0.0", true},
		{"2.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0", false}, // equal
		{"1.9.0", "1.10.0", true}, // numeric (not lexicographic) minor compare
		{"1.10.0", "1.9.0", false},
		{"0.0.1", "0.0.2", true},
		{"0.1.0", "1.0.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			got := semverLess(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("semverLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestSemverLess_Unparseable_FallsBackToLexicographic covers the branch where
// either version is not parseable and the function falls back to string
// comparison. This exercises the !okA || !okB branch in semverLess.
func TestSemverLess_Unparseable_FallsBackToLexicographic(t *testing.T) {
	// "abc" < "xyz" lexicographically.
	if !semverLess("abc", "xyz") {
		t.Errorf("semverLess(%q, %q): expected true (lexicographic fallback)", "abc", "xyz")
	}
	// "xyz" is NOT less than "abc" lexicographically.
	if semverLess("xyz", "abc") {
		t.Errorf("semverLess(%q, %q): expected false (lexicographic fallback)", "xyz", "abc")
	}
	// One invalid operand triggers fallback (mixed parseable + unparseable).
	gotLex := semverLess("notaversion", "1.0.0")
	// "notaversion" < "1.0.0" lexicographically? 'n' > '1' in ASCII so false.
	if gotLex {
		t.Errorf("semverLess(%q, %q) = true, but 'n' > '1' in ASCII, want false", "notaversion", "1.0.0")
	}
}

// TestSemverLess_BothUnparseable_StringOrder verifies that two unparseable
// versions are compared lexicographically.
func TestSemverLess_BothUnparseable_StringOrder(t *testing.T) {
	// "alpha" < "beta" lexicographically.
	if !semverLess("alpha", "beta") {
		t.Errorf("semverLess(%q, %q): expected true for lexicographic 'alpha'<'beta'", "alpha", "beta")
	}
	if semverLess("beta", "alpha") {
		t.Errorf("semverLess(%q, %q): expected false for lexicographic 'beta'>'alpha'", "beta", "alpha")
	}
}

// TestSortVersions_SemverAscending verifies sortVersions using the helper
// directly on a representative mixed input.
func TestSortVersions_WithPreRelease(t *testing.T) {
	versions := []manifest.CapturedOrbVersion{
		{Version: "2.0.0"},
		{Version: "1.2.3-beta"},
		{Version: "1.2.3"},
		{Version: "0.1.0"},
	}
	sortVersions(versions)

	// Pre-release suffix stripped → 1.2.3-beta sorts with same tuple as 1.2.3.
	// Both compare as equal so their relative order is stable (SliceStable).
	// The important invariant is that 0.1.0 is first and 2.0.0 is last.
	if versions[0].Version != "0.1.0" {
		t.Errorf("first version = %q, want %q", versions[0].Version, "0.1.0")
	}
	if versions[len(versions)-1].Version != "2.0.0" {
		t.Errorf("last version = %q, want %q", versions[len(versions)-1].Version, "2.0.0")
	}
}
