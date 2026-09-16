package ui

import "testing"

// TestTheLargestRowBuildsItsChildrenInOneAllocation is the claim
// [RowView.parts] makes about itself: "the slice is one allocation whatever
// the combination of options".
//
// It is an internal test because parts is unexported and because the property
// is about the slice and not about anything a caller can see. A row with six
// children laid out from a slice of capacity five is indistinguishable on the
// screen from one laid out from a slice of capacity six; the only difference
// is a regrow and a copy, once per row, on every rebuild.
//
// The fixture is the largest row this type can produce — every one of the six
// appends taken — and it is exactly the shape the documentation of [RowView]
// uses in its own example with an accessory added.
//
// # Why it reads the capacity and does not count allocations
//
// Because counting them cannot see the one allocation in question. Each of
// those six children is itself built by a chain of builder calls, and the
// widest row allocates nine times per call whatever the capacity is; the
// regrow is one of nine and a threshold around it would be a threshold around
// noise. The capacity is the direct observation and it is exact: a slice made
// with the right capacity and appended to exactly that many times comes back
// with cap == len, and one that had to grow comes back with the larger
// capacity Go's growth function chose. At a capacity of five this reports
// ten.
func TestTheLargestRowBuildsItsChildrenInOneAllocation(t *testing.T) {
	widest := Row("Wi-Fi").
		Icon(Symbol{id: 1, box: 24}).
		Subtitle("kiosk-net").
		Value("connected").
		Accessory(Box()).
		Chevron(Symbol{id: 2, box: 24})

	// The fixture has to take every append, or the count below would be
	// satisfied by a row that simply has fewer children than the capacity.
	if got := len(widest.parts()); got != 6 {
		t.Fatalf("the widest row this type can produce has %d children, not the 6 the capacity "+
			"in RowView.parts is claimed to be exact for. Either an append was added or one "+
			"was removed; the capacity and this test both have to follow.", got)
	}

	parts := widest.parts()
	if got := cap(parts); got != len(parts) {
		t.Errorf("the children of the widest row came back in a slice of length %d and capacity "+
			"%d. The capacity in RowView.parts is supposed to be exact for this row, so the "+
			"appends must never grow it; a capacity above the length is the regrow and the "+
			"copy that the documentation says cannot happen.", len(parts), got)
	}
}
