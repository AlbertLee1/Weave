package actions

import (
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
)

// ErrDerivedPropertyReadOnly is returned when an Action rule tries to produce
// an edit that writes to a property marked Derived in the OMS schema. Derived
// properties are computed at query time (withProperties) and have no stable
// stored representation — treating a write as a no-op would silently lie to
// the caller, so we fail loudly. Error name matches Foundry convention.
var ErrDerivedPropertyReadOnly = errors.New("DerivedPropertyReadOnly: derived properties cannot be written by Actions")

// ValidateEditsAgainstDerived enforces the US-004 read-only contract across a
// list of edits. derivedByType maps object type apiName -> set of derived
// property apiNames for that type. Delete edits never carry Properties and
// therefore always pass. Unknown object types are treated as having no
// derived properties (the caller is expected to pre-populate the map with
// real schema information).
func ValidateEditsAgainstDerived(edits []funnel.Edit, derivedByType map[string]map[string]bool) error {
	if len(derivedByType) == 0 {
		return nil
	}

	for i := range edits {
		edit := &edits[i]
		if edit.Type == funnel.EditTypeDelete {
			continue
		}
		derived := derivedByType[edit.ObjectType]
		if len(derived) == 0 {
			continue
		}
		for name := range edit.Properties {
			if derived[name] {
				return fmt.Errorf("%w: objectType=%s property=%s", ErrDerivedPropertyReadOnly, edit.ObjectType, name)
			}
		}
	}
	return nil
}
