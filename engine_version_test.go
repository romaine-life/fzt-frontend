package frontend

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// A binary built against this module reports the fzt version go.mod requires as its
// engine version, read from the build with no -X flag. A test binary records no
// dependencies, so the test builds one.
func TestEngineVersionIsTheRequiredFzt(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	want := regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/romaine-life/fzt\s+(v\S+)`).FindSubmatch(mod)
	if want == nil {
		t.Fatal("go.mod requires no github.com/romaine-life/fzt")
	}
	bin := filepath.Join(t.TempDir(), "engineversion.exe")
	if out, err := exec.Command("go", "build", "-o", bin, "testdata/engineversion.go").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want[1]) {
		t.Fatalf("engine version %q, go.mod requires %s", got, want[1])
	}
}
