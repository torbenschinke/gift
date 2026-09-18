//go:build giftauto

package main

// The opt-in half of the automation interface: a side-effect import behind a
// build tag.
//
// The program above is not changed by it in any way — no flag, no handler, no
// goroutine of its own — and an ordinary `go build ./cmd/example-kitchensink`
// does not compile this file at all. With the tag the import starts an HTTP
// interface on loopback that can drive this very window:
//
//	go build -tags giftauto -o /tmp/ks ./cmd/example-kitchensink
//	/tmp/ks &
//	curl -s 'localhost:7391/query?text=Settings' | jq '.nodes[0].centre'
//
// See the gift/auto package documentation, which also states why this must
// never be in a shipped kiosk binary.
import _ "github.com/worldiety/gift/auto"
