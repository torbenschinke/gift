//go:build giftdebug

package gift_test

// isDebugBuild reports whether the giftdebug build tag is set. The allocation
// tests use it to skip the checks that only exist in a debug build.
const isDebugBuild = true
