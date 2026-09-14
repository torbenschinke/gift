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

// RegisteredTypeNames appends the names of every registered view type to dst
// and returns the extended slice.
//
// It exists for diagnostics and for one specific test: the modifier check the
// project plan, section 4, requires instead of a generator needs a way to
// notice that a *new* view type appeared. Reflection cannot enumerate the
// types of a package, but every view type has to register itself here to be
// reconcilable at all, so this registry is the one place that knows the
// complete list.
//
// The order is registration order and the names are the ones passed to
// [RegisterType]. Never persist them.
func RegisteredTypeNames(dst []string) []string {
	typeMu.RLock()
	defer typeMu.RUnlock()
	return append(dst, typeNames[1:]...)
}
