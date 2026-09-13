package gift

import (
	"fmt"
	"sync"
)

// TypeID is a process wide stable identifier of a concrete view type.
//
// It replaces reflection in the reconciliation path: comparing two uint32
// values decides whether an existing node can be updated or has to be
// remounted. IDs are assigned in registration order and are therefore stable
// within a process, but not across processes. Never persist them.
//
// The zero TypeID is invalid and belongs to no type.
type TypeID uint32

var (
	typeMu    sync.RWMutex
	typeNames = []string{"<invalid>"}
	typeIDs   = map[string]TypeID{}
)

// RegisterType registers a view type under the given name and returns its
// [TypeID]. It is meant to be called from a package level variable
// initialiser, once per concrete view type:
//
//	var textType = gift.RegisterType("ui.Text")
//
// Registering the same name twice panics. A duplicate name means two view
// types would be considered interchangeable by the reconciler, which silently
// preserves the wrong node and the wrong state.
func RegisterType(name string) TypeID {
	typeMu.Lock()
	defer typeMu.Unlock()
	if _, dup := typeIDs[name]; dup {
		panic(fmt.Sprintf("gift: view type %q is already registered", name))
	}
	id := TypeID(len(typeNames))
	typeNames = append(typeNames, name)
	typeIDs[name] = id
	return id
}

// TypeName returns the name a type was registered under, or a placeholder for
// an unknown ID. It is meant for diagnostics only.
func TypeName(id TypeID) string {
	typeMu.RLock()
	defer typeMu.RUnlock()
	if int(id) >= len(typeNames) {
		return fmt.Sprintf("<unregistered type %d>", uint32(id))
	}
	return typeNames[id]
}
