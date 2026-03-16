package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate")

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// --- Ontology ---

func (r *PGRepository) CreateOntology(ctx context.Context, o *Ontology) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name, description)
		 VALUES ($1, $2, $3, $4)`,
		o.RID, o.APIName, o.DisplayName, o.Description)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetOntology(ctx context.Context, rid string) (*Ontology, error) {
	o := &Ontology{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, api_name, display_name, COALESCE(description, ''), created_at, updated_at
		 FROM ontologies WHERE rid = $1`, rid).
		Scan(&o.RID, &o.APIName, &o.DisplayName, &o.Description, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return o, nil
}

func (r *PGRepository) ListOntologies(ctx context.Context) ([]Ontology, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, api_name, display_name, COALESCE(description, ''), created_at, updated_at
		 FROM ontologies ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Ontology
	for rows.Next() {
		var o Ontology
		if err := rows.Scan(&o.RID, &o.APIName, &o.DisplayName, &o.Description, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, nil
}

// --- ObjectType ---

func (r *PGRepository) CreateObjectType(ctx context.Context, ot *ObjectType) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO object_types (rid, ontology_rid, api_name, display_name, plural_display_name,
		 description, primary_key_prop, title_property, status, visibility, icon_name, color)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		ot.RID, ot.OntologyRID, ot.APIName, ot.DisplayName, ot.PluralDisplayName,
		ot.Description, ot.PrimaryKey, ot.TitleProperty, ot.Status, ot.Visibility,
		ot.IconName, ot.Color)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetObjectType(ctx context.Context, rid string) (*ObjectType, error) {
	ot := &ObjectType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(plural_display_name, ''),
		 COALESCE(description, ''), primary_key_prop, COALESCE(title_property, ''),
		 COALESCE(status, 'ACTIVE'), COALESCE(visibility, 'NORMAL'),
		 COALESCE(icon_name, ''), COALESCE(color, ''), created_at, updated_at
		 FROM object_types WHERE rid = $1`, rid).
		Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.CreatedAt, &ot.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Load properties
	props, err := r.ListProperties(ctx, ot.RID)
	if err != nil {
		return nil, err
	}
	ot.Properties = props

	return ot, nil
}

func (r *PGRepository) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error) {
	var rid string
	err := r.pool.QueryRow(ctx,
		`SELECT rid FROM object_types WHERE ontology_rid = $1 AND api_name = $2`,
		ontologyRID, apiName).Scan(&rid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r.GetObjectType(ctx, rid)
}

func (r *PGRepository) ListObjectTypes(ctx context.Context, ontologyRID string) ([]ObjectType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(plural_display_name, ''),
		 COALESCE(description, ''), primary_key_prop, COALESCE(title_property, ''),
		 COALESCE(status, 'ACTIVE'), COALESCE(visibility, 'NORMAL'),
		 COALESCE(icon_name, ''), COALESCE(color, ''), created_at, updated_at
		 FROM object_types WHERE ontology_rid = $1 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ObjectType
	for rows.Next() {
		var ot ObjectType
		if err := rows.Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.CreatedAt, &ot.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, ot)
	}
	return result, nil
}

func (r *PGRepository) UpdateObjectType(ctx context.Context, ot *ObjectType) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE object_types SET display_name=$1, plural_display_name=$2, description=$3,
		 title_property=$4, status=$5, visibility=$6, icon_name=$7, color=$8, updated_at=now()
		 WHERE rid=$9`,
		ot.DisplayName, ot.PluralDisplayName, ot.Description,
		ot.TitleProperty, ot.Status, ot.Visibility, ot.IconName, ot.Color, ot.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteObjectType(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM object_types WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Property ---

func (r *PGRepository) CreateProperty(ctx context.Context, p *Property) error {
	typeConfig := p.TypeConfig
	if len(typeConfig) == 0 {
		typeConfig = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO properties (rid, object_type_rid, api_name, display_name, description,
		 base_type, type_config, is_array, is_nullable, is_searchable, is_sortable)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		p.RID, p.ObjectTypeRID, p.APIName, p.DisplayName, p.Description,
		p.BaseType, typeConfig, p.IsArray, p.IsNullable, p.IsSearchable, p.IsSortable)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, is_nullable, is_searchable, is_sortable, created_at
		 FROM properties WHERE object_type_rid = $1 ORDER BY api_name`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Property
	for rows.Next() {
		var p Property
		if err := rows.Scan(&p.RID, &p.ObjectTypeRID, &p.APIName, &p.DisplayName, &p.Description,
			&p.BaseType, &p.TypeConfig, &p.IsArray, &p.IsNullable, &p.IsSearchable, &p.IsSortable,
			&p.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *PGRepository) DeleteProperty(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM properties WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- LinkType ---

func (r *PGRepository) CreateLinkType(ctx context.Context, lt *LinkType) error {
	fkConfig := lt.ForeignKeyConfig
	if len(fkConfig) == 0 {
		fkConfig = nil
	}
	jtConfig := lt.JoinTableConfig
	if len(jtConfig) == 0 {
		jtConfig = nil
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO link_types (rid, ontology_rid, api_name, display_name, description,
		 source_object_type, target_object_type, cardinality, foreign_key_config, join_table_config, is_required)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		lt.RID, lt.OntologyRID, lt.APIName, lt.DisplayName, lt.Description,
		lt.SourceObjectType, lt.TargetObjectType, lt.Cardinality,
		fkConfig, jtConfig, lt.IsRequired)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetLinkType(ctx context.Context, rid string) (*LinkType, error) {
	lt := &LinkType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 source_object_type, target_object_type, cardinality,
		 foreign_key_config, join_table_config, is_required, created_at
		 FROM link_types WHERE rid = $1`, rid).
		Scan(&lt.RID, &lt.OntologyRID, &lt.APIName, &lt.DisplayName, &lt.Description,
			&lt.SourceObjectType, &lt.TargetObjectType, &lt.Cardinality,
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired, &lt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return lt, nil
}

func (r *PGRepository) ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 source_object_type, target_object_type, cardinality,
		 foreign_key_config, join_table_config, is_required, created_at
		 FROM link_types WHERE source_object_type = $1 ORDER BY api_name`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []LinkType
	for rows.Next() {
		var lt LinkType
		if err := rows.Scan(&lt.RID, &lt.OntologyRID, &lt.APIName, &lt.DisplayName, &lt.Description,
			&lt.SourceObjectType, &lt.TargetObjectType, &lt.Cardinality,
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired, &lt.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, lt)
	}
	return result, nil
}

// --- ActionType ---

func (r *PGRepository) CreateActionType(ctx context.Context, at *ActionType) error {
	params := at.Parameters
	if len(params) == 0 {
		params = json.RawMessage(`[]`)
	}
	rules := at.Rules
	if len(rules) == 0 {
		rules = json.RawMessage(`[]`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO action_types (rid, ontology_rid, api_name, display_name, description,
		 status, parameters, rules, function_rid, is_function_backed)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		at.RID, at.OntologyRID, at.APIName, at.DisplayName, at.Description,
		at.Status, params, rules, at.FunctionRID, at.IsFunctionBacked)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetActionType(ctx context.Context, rid string) (*ActionType, error) {
	at := &ActionType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(status, 'ACTIVE'), parameters, rules,
		 COALESCE(function_rid, ''), is_function_backed, created_at
		 FROM action_types WHERE rid = $1`, rid).
		Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return at, nil
}

func (r *PGRepository) ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(status, 'ACTIVE'), parameters, rules,
		 COALESCE(function_rid, ''), is_function_backed, created_at
		 FROM action_types WHERE ontology_rid = $1 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ActionType
	for rows.Next() {
		var at ActionType
		if err := rows.Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, at)
	}
	return result, nil
}

func (r *PGRepository) UpdateActionType(ctx context.Context, at *ActionType) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE action_types SET display_name=$1, description=$2, status=$3,
		 parameters=$4, rules=$5 WHERE rid=$6`,
		at.DisplayName, at.Description, at.Status, at.Parameters, at.Rules, at.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Interface ---

func (r *PGRepository) CreateInterface(ctx context.Context, iface *Interface) error {
	sharedProps := iface.SharedProperties
	if len(sharedProps) == 0 {
		sharedProps = json.RawMessage(`[]`)
	}
	var extendsRID *string
	if iface.ExtendsRID != "" {
		extendsRID = &iface.ExtendsRID
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO interfaces (rid, ontology_rid, api_name, display_name, extends_rid, shared_properties)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		iface.RID, iface.OntologyRID, iface.APIName, iface.DisplayName, extendsRID, sharedProps)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListInterfaces(ctx context.Context, ontologyRID string) ([]Interface, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(extends_rid, ''),
		 COALESCE(shared_properties, '[]'), created_at
		 FROM interfaces WHERE ontology_rid = $1 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Interface
	for rows.Next() {
		var i Interface
		if err := rows.Scan(&i.RID, &i.OntologyRID, &i.APIName, &i.DisplayName,
			&i.ExtendsRID, &i.SharedProperties, &i.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, nil
}

func (r *PGRepository) AttachInterface(ctx context.Context, oti *ObjectTypeInterface) error {
	propMapping := oti.PropertyMapping
	if len(propMapping) == 0 {
		propMapping = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO object_type_interfaces (object_type_rid, interface_rid, property_mapping)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (object_type_rid, interface_rid) DO UPDATE SET property_mapping = $3`,
		oti.ObjectTypeRID, oti.InterfaceRID, propMapping)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// wrapPGError maps common PG errors to domain errors.
func wrapPGError(err error) error {
	if err == nil {
		return nil
	}
	errMsg := err.Error()
	// Check for unique constraint violation (23505)
	if contains(errMsg, "23505") || contains(errMsg, "duplicate key") {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	return err
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
