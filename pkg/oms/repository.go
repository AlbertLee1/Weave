package oms

import "context"

// Repository defines the data access interface for ontology metadata.
type Repository interface {
	// Ontology
	CreateOntology(ctx context.Context, o *Ontology) error
	GetOntology(ctx context.Context, rid string) (*Ontology, error)
	ListOntologies(ctx context.Context) ([]Ontology, error)

	// ObjectType
	CreateObjectType(ctx context.Context, ot *ObjectType) error
	GetObjectType(ctx context.Context, rid string) (*ObjectType, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error)
	ListObjectTypes(ctx context.Context, ontologyRID string) ([]ObjectType, error)
	UpdateObjectType(ctx context.Context, ot *ObjectType) error
	DeleteObjectType(ctx context.Context, rid string) error

	// Property
	CreateProperty(ctx context.Context, p *Property) error
	ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error)
	DeleteProperty(ctx context.Context, rid string) error

	// LinkType
	CreateLinkType(ctx context.Context, lt *LinkType) error
	GetLinkType(ctx context.Context, rid string) (*LinkType, error)
	ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error)

	// ActionType
	CreateActionType(ctx context.Context, at *ActionType) error
	GetActionType(ctx context.Context, rid string) (*ActionType, error)
	ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error)
	UpdateActionType(ctx context.Context, at *ActionType) error

	// Interface
	CreateInterface(ctx context.Context, iface *Interface) error
	ListInterfaces(ctx context.Context, ontologyRID string) ([]Interface, error)
	AttachInterface(ctx context.Context, oti *ObjectTypeInterface) error
}
