package auto

import (
	"os/exec"
	"strings"
	"testing"
)

// This file has no build tag on purpose: it is the test of the claim that
// without the giftauto tag this package is nothing at all.
//
// The claim is not "it is small". It is that the package brings in no
// dependency, starts no goroutine and contributes no symbol, so that an
// accidental import into a kiosk binary cannot smuggle an HTTP server into a
// product. That is checked the only way it can honestly be checked: by asking
// the toolchain what the package depends on, with the tag and without it.

// TestWithoutTheBuildTagThePackageDependsOnNothingAtAll is the no-weight
// claim. It fails the moment a file without the giftauto tag imports anything.
func TestWithoutTheBuildTagThePackageDependsOnNothingAtAll(t *testing.T) {
	deps := listDeps(t, "", "github.com/worldiety/gift/auto")
	if len(deps) != 1 || deps[0] != "github.com/worldiety/gift/auto" {
		t.Fatalf("without the tag the package depends on %v; it must depend on nothing, "+
			"because a package that pulls in net/http is a package that can smuggle a "+
			"server into a production binary", deps)
	}
}

// TestWithTheBuildTagThePackageBringsTheServerIn is the other half, and it is
// what makes the test above meaningful: the difference between the two lists
// is exactly the weight the tag adds.
func TestWithTheBuildTagThePackageBringsTheServerIn(t *testing.T) {
	deps := listDeps(t, "giftauto", "github.com/worldiety/gift/auto")
	want := []string{"net/http", "encoding/json", "image/png",
		"github.com/worldiety/gift"}
	for _, w := range want {
		if !contains(deps, w) {
			t.Fatalf("with the tag the package does not depend on %s; the list was %v", w, deps)
		}
	}
	if contains(deps, "github.com/hajimehoshi/ebiten/v2") {
		t.Fatal("the automation driver must not depend on the backend")
	}
	if n := len(deps); n < 50 {
		t.Fatalf("the tagged package has %d dependencies, which is too few to be the "+
			"whole server; the tag is probably not being applied", n)
	}
}

func TestBackendIncludesAutomationOnlyWithTheBuildTag(t *testing.T) {
	for _, tags := range []string{"", "giftauto"} {
		t.Run("tags="+tags, func(t *testing.T) {
			deps := listDeps(t, tags, "github.com/worldiety/gift/backend/ebiten")
			if got := contains(deps, "github.com/worldiety/gift/auto"); got != (tags == "giftauto") {
				t.Fatalf("backend includes automation = %v with tags %q", got, tags)
			}
		})
	}
}

// listDeps asks the toolchain for a package's transitive imports.
func listDeps(t *testing.T, tags, pkg string) []string {
	t.Helper()
	args := []string{"list", "-deps"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, pkg)
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	return strings.Fields(string(out))
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
