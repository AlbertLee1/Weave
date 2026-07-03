package objectset

import (
	"context"
	"sort"
)

// InterfacePropertyMappingResolver is an optional, richer capability the
// executor's wired InterfaceResolver may also satisfy. Given an interface
// apiName it returns, per implementing ObjectType (keyed by objectType
// apiName), the SharedPropertyType apiName -> local property apiName mapping.
//
// This is the data behind Foundry's interfaceToObjectTypeMappings field on the
// loadObjectsMultipleObjectTypes / loadObjectsOrInterfaces responses. Foundry's
// InterfaceToObjectTypeMapping is Dict[SharedPropertyTypeApiName, PropertyApiName]
// and InterfaceToObjectTypeMappings is Dict[ObjectTypeApiName, ...], so the full
// wire field is Dict[InterfaceApiName, Dict[ObjectTypeApiName, Dict[SPT, Prop]]].
//
// In production the mapping is sourced from the OMS
// ObjectTypeInterface.PropertyMapping column. The capability is kept as a narrow
// optional interface (discovered by type assertion on the already-wired
// InterfaceResolver) so pkg/oss/objectset does not take a direct pkg/oms
// dependency.
type InterfacePropertyMappingResolver interface {
	ResolveInterfacePropertyMappings(ctx context.Context, interfaceAPIName string) (map[string]map[string]string, error)
}

// collectInterfaceTypes walks an ObjectSet definition tree and returns the
// deduplicated, sorted set of interface apiNames referenced by any
// "interfaceBase" node. An ObjectSet whose type scope contains no interfaces
// (for example a plain base objectType query) yields an empty slice, which the
// caller treats as "omit interfaceToObjectTypeMappings" — matching Foundry,
// where the field is only populated when the returned object set's type scope
// includes interfaces.
func collectInterfaceTypes(def *Definition) []string {
	seen := map[string]struct{}{}
	var walk func(d *Definition)
	walk = func(d *Definition) {
		if d == nil {
			return
		}
		if d.Type == "interfaceBase" && d.InterfaceType != "" {
			seen[d.InterfaceType] = struct{}{}
		}
		walk(d.ObjectSet)
		for _, child := range d.ObjectSets {
			walk(child)
		}
	}
	walk(def)

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildInterfaceToObjectTypeMappings assembles the Foundry
// interfaceToObjectTypeMappings wire field:
//
//	{ interfaceApiName: { objectTypeApiName: { sptApiName: propertyApiName } } }
//
// It returns nil (so the caller omits the field) when the resolver is absent,
// no interfaces are involved, or none of the involved interfaces resolve to any
// implementing object type. Interfaces that fail to resolve are skipped rather
// than failing the whole response, since these mappings are advisory metadata
// for polymorphic OSDK clients — not part of the object payload itself.
func buildInterfaceToObjectTypeMappings(ctx context.Context, resolver InterfacePropertyMappingResolver, interfaceNames []string) map[string]map[string]map[string]string {
	if resolver == nil || len(interfaceNames) == 0 {
		return nil
	}

	out := make(map[string]map[string]map[string]string, len(interfaceNames))
	for _, ifaceName := range interfaceNames {
		perType, err := resolver.ResolveInterfacePropertyMappings(ctx, ifaceName)
		if err != nil || len(perType) == 0 {
			continue
		}
		typeMap := make(map[string]map[string]string, len(perType))
		for objectType, sptMap := range perType {
			// Copy the inner map so a caller cannot mutate the resolver's
			// backing store and so a nil mapping serializes as {} not null.
			cp := make(map[string]string, len(sptMap))
			for spt, prop := range sptMap {
				cp[spt] = prop
			}
			typeMap[objectType] = cp
		}
		out[ifaceName] = typeMap
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
