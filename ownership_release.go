//go:build !giftdebug

package gift

import "github.com/worldiety/gift/internal/scene"

// ownershipGuard is empty unless the giftdebug build tag is set.
//
// The release version of the slice ownership check exists so that the call
// site in the reconciler stays identical in both builds. Neither the type nor
// the function leaves a single instruction behind: an empty struct occupies no
// space and an empty function is inlined away.
type ownershipGuard struct{}

// checkOwnership does nothing in a release build. Build with -tags giftdebug
// to detect a caller that keeps modifying a children slice it handed to gift.
func checkOwnership(*nodeData, []View) {}

// releaseOwnership does nothing in a release build.
func (*nodeData) releaseOwnership() {}

// checkDuplicateKeys does nothing in a release build. Build with
// -tags giftdebug to have duplicate sibling keys diagnosed.
func checkDuplicateKeys(*App, scene.Handle, []scene.Handle) {}
