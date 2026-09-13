//go:build giftdebug

package gift

import (
	"fmt"
	"reflect"
)

// ownershipGuard remembers the children slice a build handed over together
// with the dynamic type of every element, so that the next build can prove
// that the caller did not keep writing into it.
//
// Reflection and the extra allocations are acceptable here because this file
// only exists in a build with the giftdebug tag.
//
// Limits of the check, stated honestly: gift keeps its own copy of the slice
// header, so a caller that reslices or appends to its own variable afterwards
// is invisible here, and in fact harmless, because it cannot change what gift
// sees. What is detected is the case that actually corrupts the tree: the
// caller overwriting elements inside the range it gave away, which is what
// happens when a reused scratch buffer is passed to a view constructor.
type ownershipGuard struct {
	valid bool
	slice []View
	types []reflect.Type
}

// checkOwnership verifies that the elements of the previously handed over
// children slice are still the ones gift was given, and then records the new
// slice.
//
// Element.Children belongs to gift from the moment it is returned; see the
// project plan, section 4. The resulting bug, a tree that quietly describes
// something else than the code at the call site says, is otherwise very hard
// to find, which is why it is a panic and not a log line.
func checkOwnership(nd *nodeData, next []View) {
	if g := &nd.own; g.valid {
		for i := range g.slice {
			if got := reflect.TypeOf(g.slice[i]); got != g.types[i] {
				panic(fmt.Sprintf(
					"gift: element %d of the children slice of view %T was replaced after it was handed to gift: %v became %v; "+
						"the slice belongs to gift from the moment Build returns",
					i, nd.view, g.types[i], got))
			}
		}
	}
	record(&nd.own, next)
}

func record(g *ownershipGuard, s []View) {
	g.valid = true
	g.slice = s
	g.types = g.types[:0]
	for _, v := range s {
		g.types = append(g.types, reflect.TypeOf(v))
	}
}
