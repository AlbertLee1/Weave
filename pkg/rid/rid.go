package rid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// RID represents a parsed Resource Identifier.
//
// Version is the optional @vN suffix (US-070). Empty when the RID
// has no version pin; otherwise the decimal version digits without
// leading zero. Two RIDs with the same {Service, Realm, ResourceType,
// ID} but different Version are NOT equal — explicit @vN means "this
// specific historical version" while empty means "latest", and Action
// / Snapshot endpoints will route on that distinction.
type RID struct {
	Service      string
	Realm        string
	ResourceType string
	ID           string
	Version      string
}

// versionPattern matches a canonical version digit string: positive
// decimal, no leading zero. Rejects "0", "0123" so persisted RIDs stay
// byte-identical across reads (same rationale as uuidPattern).
var versionPattern = regexp.MustCompile(`^[1-9]\d*$`)

// uuidPattern is the canonical RFC 4122 textual form, lowercase only.
// We intentionally reject uppercase to keep persisted RIDs byte-identical
// across read paths (callers compare RIDs by string equality everywhere).
var uuidPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

// validSegment reports whether s is a non-empty service/realm/resource-type
// segment. Control characters (CR/LF/TAB/NUL/DEL/...) and embedded dots are
// rejected — the former because RIDs frequently flow through log lines / URLs
// where injecting a CR is a known escape vector, the latter because '.' is
// the segment separator.
func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '.' {
			return false
		}
	}
	return true
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

// NewFunctionRID generates a new RID for a function.
func NewFunctionRID() string {
	return New("ontology", "main", "function")
}

// NewQueryTypeRID generates a new RID for a query type.
func NewQueryTypeRID() string {
	return New("ontology", "main", "query-type")
}

// NewBranchRID generates a new RID for an ontology branch.
func NewBranchRID() string {
	return New("ontology", "main", "branch")
}

// NewProposalRID generates a new RID for an ontology proposal.
func NewProposalRID() string {
	return New("ontology", "main", "proposal")
}

// NewProposalReviewRID generates a new RID for a proposal review.
func NewProposalReviewRID() string {
	return New("ontology", "main", "proposal-review")
}

// NewAutomationRuleRID generates a new RID for an automation rule.
func NewAutomationRuleRID() string {
	return New("ontology", "main", "automation-rule")
}

// NewAutomationExecutionRID generates a new RID for an automation execution.
func NewAutomationExecutionRID() string {
	return New("ontology", "main", "automation-execution")
}

// NewNotificationRID generates a new RID for a notification.
func NewNotificationRID() string {
	return New("ontology", "main", "notification")
}

// IsRID reports whether s looks like a Resource Identifier (starts with "ri.").
func IsRID(s string) bool {
	return strings.HasPrefix(s, "ri.")
}

// Parse parses a RID string into its constituent parts.
// Expected format: ri.{service}.{realm}.{resourceType}.{uuid}
//
// Empty segments, control characters (including CR/LF/TAB/NUL/DEL) and
// non-canonical / non-lowercase UUIDs are rejected. Callers like
// realmFromRID intentionally treat any error as "fallback to defaults".
func Parse(rid string) (*RID, error) {
	// US-070: split optional @vN suffix off the ID segment before the
	// 5-way dot-split. We do this first so a malformed base RID still
	// errors on the base shape rather than being masked by a suffix
	// branch — callers like realmFromRID treat any error as fallback,
	// so the error message matters less than the rejection itself.
	idSegment, version, err := splitVersionSuffix(rid)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(idSegment, ".", 5)
	if len(parts) != 5 || parts[0] != "ri" {
		return nil, fmt.Errorf("invalid RID format: %q", rid)
	}
	if !validSegment(parts[1]) {
		return nil, fmt.Errorf("invalid RID service segment: %q", rid)
	}
	if !validSegment(parts[2]) {
		return nil, fmt.Errorf("invalid RID realm segment: %q", rid)
	}
	if !validSegment(parts[3]) {
		return nil, fmt.Errorf("invalid RID resource-type segment: %q", rid)
	}
	if !uuidPattern.MatchString(parts[4]) {
		return nil, fmt.Errorf("invalid RID id segment (expected lowercase canonical UUID): %q", rid)
	}
	return &RID{
		Service:      parts[1],
		Realm:        parts[2],
		ResourceType: parts[3],
		ID:           parts[4],
		Version:      version,
	}, nil
}

// splitVersionSuffix peels an optional @vN suffix off the input,
// returning (base, version, err). version is the decimal digit string
// (no "v" prefix); empty when no suffix is present. Errors when the
// suffix is malformed so we never persist garbage like "@v" or
// "@vabc".
func splitVersionSuffix(rid string) (base, version string, err error) {
	at := strings.Index(rid, "@")
	if at < 0 {
		return rid, "", nil
	}
	// Reject more than one @ — two @ would mean either a double
	// suffix (@v3@v4) or an @ inside the base, both invalid.
	if strings.LastIndex(rid, "@") != at {
		return "", "", fmt.Errorf("invalid RID version suffix (multiple @): %q", rid)
	}
	suffix := rid[at+1:]
	if !strings.HasPrefix(suffix, "v") {
		return "", "", fmt.Errorf("invalid RID version suffix (expected @vN): %q", rid)
	}
	v := suffix[1:]
	if !versionPattern.MatchString(v) {
		return "", "", fmt.Errorf("invalid RID version suffix (expected positive decimal, no leading zero): %q", rid)
	}
	return rid[:at], v, nil
}

// String returns the canonical "ri.{service}.{realm}.{resourceType}.{id}"
// form. Nil-safe: a nil receiver returns the empty string so logging code
// that prints %v on a nullable lookup result never panics.
func (r *RID) String() string {
	if r == nil {
		return ""
	}
	s := "ri." + r.Service + "." + r.Realm + "." + r.ResourceType + "." + r.ID
	if r.Version != "" {
		s += "@v" + r.Version
	}
	return s
}

// Equal reports whether two RIDs have identical fields. Nil-safe: two nil
// pointers compare equal; a nil and a non-nil compare unequal.
func (r *RID) Equal(other *RID) bool {
	if r == nil || other == nil {
		return r == other
	}
	return r.Service == other.Service &&
		r.Realm == other.Realm &&
		r.ResourceType == other.ResourceType &&
		r.ID == other.ID &&
		r.Version == other.Version
}

// Hash returns a stable SHA-256 hex digest of the canonical string form.
// Two RIDs hash to the same value iff Equal returns true.
func (r *RID) Hash() string {
	if r == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(r.String()))
	return hex.EncodeToString(sum[:])
}

// UUIDVersion returns the version digit (1-7) parsed from the id segment.
// Returns a non-nil error if the id is not a valid UUID.
func (r *RID) UUIDVersion() (int, error) {
	if r == nil {
		return 0, fmt.Errorf("nil RID")
	}
	u, err := uuid.Parse(r.ID)
	if err != nil {
		return 0, err
	}
	return int(u.Version()), nil
}

// IsUUIDv4 reports whether the id segment is a UUID version 4 (random).
// Today rid.New funnels through google/uuid.New so every RID this package
// mints is v4; v7 (time-ordered) is permitted for future-proofing.
func (r *RID) IsUUIDv4() bool {
	v, err := r.UUIDVersion()
	return err == nil && v == 4
}

// IsUUIDv7 reports whether the id segment is a UUID version 7 (time-ordered).
func (r *RID) IsUUIDv7() bool {
	v, err := r.UUIDVersion()
	return err == nil && v == 7
}
