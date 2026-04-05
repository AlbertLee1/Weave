package rid

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// RID represents a parsed Resource Identifier.
type RID struct {
	Service      string
	Realm        string
	ResourceType string
	ID           string
}

// New generates a new RID string with an auto-generated UUID.
func New(service, realm, resourceType string) string {
	return fmt.Sprintf("ri.%s.%s.%s.%s", service, realm, resourceType, uuid.New().String())
}

// NewOntologyRID generates a new RID for an ontology.
func NewOntologyRID() string {
	return New("ontology", "main", "ontology")
}

// NewObjectTypeRID generates a new RID for an object type.
func NewObjectTypeRID() string {
	return New("ontology", "main", "object-type")
}

// NewPropertyRID generates a new RID for a property.
func NewPropertyRID() string {
	return New("ontology", "main", "property")
}

// NewLinkTypeRID generates a new RID for a link type.
func NewLinkTypeRID() string {
	return New("ontology", "main", "link-type")
}

// NewObjectRID generates a new RID for an object.
func NewObjectRID() string {
	return New("ontology", "main", "object")
}

// NewActionTypeRID generates a new RID for an action type.
func NewActionTypeRID() string {
	return New("ontology", "main", "action-type")
}

// NewInterfaceRID generates a new RID for an interface.
func NewInterfaceRID() string {
	return New("ontology", "main", "interface")
}

// NewSharedPropertyRID generates a new RID for a shared property.
func NewSharedPropertyRID() string {
	return New("ontology", "main", "shared-property")
}

// NewTypeGroupRID generates a new RID for a type group.
func NewTypeGroupRID() string {
	return New("ontology", "main", "type-group")
}

// NewValueTypeRID generates a new RID for a value type.
func NewValueTypeRID() string {
	return New("ontology", "main", "value-type")
}

// NewSecurityPolicyRID generates a new RID for a security policy.
func NewSecurityPolicyRID() string {
	return New("ontology", "main", "security-policy")
}

// NewDatasourceBindingRID generates a new RID for a datasource binding.
func NewDatasourceBindingRID() string {
	return New("ontology", "main", "datasource-binding")
}

// NewQueryTypeRID generates a new RID for a query type.
func NewQueryTypeRID() string {
	return New("ontology", "main", "query-type")
}

// Parse parses a RID string into its constituent parts.
// Expected format: ri.{service}.{realm}.{resourceType}.{uuid}
func Parse(rid string) (*RID, error) {
	parts := strings.SplitN(rid, ".", 5)
	if len(parts) != 5 || parts[0] != "ri" {
		return nil, fmt.Errorf("invalid RID format: %q", rid)
	}
	return &RID{
		Service:      parts[1],
		Realm:        parts[2],
		ResourceType: parts[3],
		ID:           parts[4],
	}, nil
}
