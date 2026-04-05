package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

func (r *PGRepository) GetOntology(ctx context.Context, ridOrApiName string) (*Ontology, error) {
	o := &Ontology{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, api_name, display_name, COALESCE(description, ''), created_at, updated_at
		 FROM ontologies WHERE rid = $1 OR api_name = $1`, ridOrApiName).
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

func (r *PGRepository) UpdateOntology(ctx context.Context, o *Ontology) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontologies SET display_name=$1, description=$2, updated_at=now()
		 WHERE rid=$3`,
		o.DisplayName, o.Description, o.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		 COALESCE(icon_name, ''), COALESCE(color, ''),
		 COALESCE(deprecated_reason, ''), deprecated_deadline,
		 created_at, updated_at
		 FROM object_types WHERE rid = $1`, rid).
		Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.DeprecatedReason, &ot.DeprecatedDeadline,
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
		`SELECT rid FROM object_types
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND api_name = $2`,
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
		 COALESCE(icon_name, ''), COALESCE(color, ''),
		 COALESCE(deprecated_reason, ''), deprecated_deadline,
		 created_at, updated_at
		 FROM object_types
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
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
			&ot.DeprecatedReason, &ot.DeprecatedDeadline,
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
		 title_property=$4, status=$5, visibility=$6, icon_name=$7, color=$8,
		 deprecated_reason=$9, deprecated_deadline=$10, updated_at=now()
		 WHERE rid=$11`,
		ot.DisplayName, ot.PluralDisplayName, ot.Description,
		ot.TitleProperty, ot.Status, ot.Visibility, ot.IconName, ot.Color,
		ot.DeprecatedReason, ot.DeprecatedDeadline, ot.RID)
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
	status := p.Status
	if status == "" {
		status = "ACTIVE"
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO properties (rid, object_type_rid, api_name, display_name, description,
		 base_type, type_config, is_array, is_nullable, is_searchable, is_sortable, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.RID, p.ObjectTypeRID, p.APIName, p.DisplayName, p.Description,
		p.BaseType, typeConfig, p.IsArray, p.IsNullable, p.IsSearchable, p.IsSortable, status)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, is_nullable, is_searchable, is_sortable,
		 COALESCE(status, 'ACTIVE'), COALESCE(deprecated_reason, ''), created_at
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
			&p.Status, &p.DeprecatedReason, &p.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *PGRepository) GetProperty(ctx context.Context, rid string) (*Property, error) {
	p := &Property{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, object_type_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, is_nullable, is_searchable, is_sortable,
		 COALESCE(status, 'ACTIVE'), COALESCE(deprecated_reason, ''), created_at
		 FROM properties WHERE rid = $1`, rid).
		Scan(&p.RID, &p.ObjectTypeRID, &p.APIName, &p.DisplayName, &p.Description,
			&p.BaseType, &p.TypeConfig, &p.IsArray, &p.IsNullable, &p.IsSearchable, &p.IsSortable,
			&p.Status, &p.DeprecatedReason, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *PGRepository) UpdateProperty(ctx context.Context, p *Property) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE properties SET display_name=$1, description=$2,
		 is_searchable=$3, is_sortable=$4, is_nullable=$5,
		 status=$6, deprecated_reason=$7
		 WHERE rid=$8`,
		p.DisplayName, p.Description, p.IsSearchable, p.IsSortable, p.IsNullable,
		p.Status, p.DeprecatedReason, p.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

func (r *PGRepository) ListLinkTypes(ctx context.Context, ontologyRID string) ([]LinkType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 source_object_type, target_object_type, cardinality,
		 foreign_key_config, join_table_config, is_required, created_at
		 FROM link_types
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
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

func (r *PGRepository) UpdateLinkType(ctx context.Context, lt *LinkType) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE link_types SET display_name=$1, description=$2, is_required=$3
		 WHERE rid=$4`,
		lt.DisplayName, lt.Description, lt.IsRequired, lt.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteLinkType(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM link_types WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
	sc := at.SubmissionCriteria
	if len(sc) == 0 {
		sc = json.RawMessage(`[]`)
	}
	se := at.SideEffects
	if len(se) == 0 {
		se = json.RawMessage(`[]`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO action_types (rid, ontology_rid, api_name, display_name, description,
		 status, parameters, rules, function_rid, is_function_backed, submission_criteria, side_effects)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		at.RID, at.OntologyRID, at.APIName, at.DisplayName, at.Description,
		at.Status, params, rules, at.FunctionRID, at.IsFunctionBacked, sc, se)
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
		 COALESCE(function_rid, ''), is_function_backed, created_at,
		 submission_criteria, side_effects
		 FROM action_types WHERE rid = $1`, rid).
		Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt,
			&at.SubmissionCriteria, &at.SideEffects)
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
		 COALESCE(function_rid, ''), is_function_backed, created_at,
		 submission_criteria, side_effects
		 FROM action_types
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ActionType
	for rows.Next() {
		var at ActionType
		if err := rows.Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt,
			&at.SubmissionCriteria, &at.SideEffects); err != nil {
			return nil, err
		}
		result = append(result, at)
	}
	return result, nil
}

func (r *PGRepository) UpdateActionType(ctx context.Context, at *ActionType) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE action_types SET display_name=$1, description=$2, status=$3,
		 parameters=$4, rules=$5, submission_criteria=$6, side_effects=$7 WHERE rid=$8`,
		at.DisplayName, at.Description, at.Status, at.Parameters, at.Rules,
		at.SubmissionCriteria, at.SideEffects, at.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteActionType(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM action_types WHERE rid = $1`, rid)
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

func (r *PGRepository) GetInterface(ctx context.Context, rid string) (*Interface, error) {
	i := &Interface{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(extends_rid, ''),
		 COALESCE(shared_properties, '[]'), created_at
		 FROM interfaces WHERE rid = $1`, rid).
		Scan(&i.RID, &i.OntologyRID, &i.APIName, &i.DisplayName,
			&i.ExtendsRID, &i.SharedProperties, &i.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return i, nil
}

func (r *PGRepository) GetInterfaceByAPIName(ctx context.Context, ontologyRID, apiName string) (*Interface, error) {
	i := &Interface{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(extends_rid, ''),
		 COALESCE(shared_properties, '[]'), created_at
		 FROM interfaces
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND api_name = $2`, ontologyRID, apiName).
		Scan(&i.RID, &i.OntologyRID, &i.APIName, &i.DisplayName,
			&i.ExtendsRID, &i.SharedProperties, &i.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return i, nil
}

func (r *PGRepository) ListInterfaces(ctx context.Context, ontologyRID string) ([]Interface, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(extends_rid, ''),
		 COALESCE(shared_properties, '[]'), created_at
		 FROM interfaces
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
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

func (r *PGRepository) UpdateInterface(ctx context.Context, iface *Interface) error {
	var extendsRID *string
	if iface.ExtendsRID != "" {
		extendsRID = &iface.ExtendsRID
	}
	sharedProps := iface.SharedProperties
	if len(sharedProps) == 0 {
		sharedProps = json.RawMessage(`[]`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE interfaces SET display_name=$1, extends_rid=$2, shared_properties=$3
		 WHERE rid=$4`,
		iface.DisplayName, extendsRID, sharedProps, iface.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteInterface(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM interfaces WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

func (r *PGRepository) DetachInterface(ctx context.Context, objectTypeRID, interfaceRID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM object_type_interfaces WHERE object_type_rid = $1 AND interface_rid = $2`,
		objectTypeRID, interfaceRID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) ListObjectTypeInterfaces(ctx context.Context, objectTypeRID string) ([]ObjectTypeInterface, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT object_type_rid, interface_rid, COALESCE(property_mapping, '{}')
		 FROM object_type_interfaces WHERE object_type_rid = $1`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ObjectTypeInterface
	for rows.Next() {
		var oti ObjectTypeInterface
		if err := rows.Scan(&oti.ObjectTypeRID, &oti.InterfaceRID, &oti.PropertyMapping); err != nil {
			return nil, err
		}
		result = append(result, oti)
	}
	return result, nil
}

func (r *PGRepository) ListInterfaceObjectTypes(ctx context.Context, interfaceRID string) ([]ObjectType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ot.rid, ot.ontology_rid, ot.api_name, ot.display_name, COALESCE(ot.plural_display_name, ''),
		 COALESCE(ot.description, ''), ot.primary_key, COALESCE(ot.title_property, ''),
		 COALESCE(ot.status, 'ACTIVE'), COALESCE(ot.visibility, 'NORMAL'),
		 COALESCE(ot.icon, ''), COALESCE(ot.color, ''), ot.created_at
		 FROM object_types ot
		 JOIN object_type_interfaces oti ON ot.rid = oti.object_type_rid
		 WHERE oti.interface_rid = $1
		 ORDER BY ot.api_name`, interfaceRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ObjectType
	for rows.Next() {
		var ot ObjectType
		if err := rows.Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility,
			&ot.IconName, &ot.Color, &ot.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, ot)
	}
	return result, nil
}

// --- SharedProperty ---

func (r *PGRepository) CreateSharedProperty(ctx context.Context, sp *SharedProperty) error {
	typeConfig := sp.TypeConfig
	if len(typeConfig) == 0 {
		typeConfig = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO shared_properties (rid, ontology_rid, api_name, display_name, description,
		 base_type, type_config, is_array)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sp.RID, sp.OntologyRID, sp.APIName, sp.DisplayName, sp.Description,
		sp.BaseType, typeConfig, sp.IsArray)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetSharedProperty(ctx context.Context, rid string) (*SharedProperty, error) {
	sp := &SharedProperty{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, created_at
		 FROM shared_properties WHERE rid = $1`, rid).
		Scan(&sp.RID, &sp.OntologyRID, &sp.APIName, &sp.DisplayName, &sp.Description,
			&sp.BaseType, &sp.TypeConfig, &sp.IsArray, &sp.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sp, nil
}

func (r *PGRepository) ListSharedProperties(ctx context.Context, ontologyRID string) ([]SharedProperty, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, created_at
		 FROM shared_properties
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SharedProperty
	for rows.Next() {
		var sp SharedProperty
		if err := rows.Scan(&sp.RID, &sp.OntologyRID, &sp.APIName, &sp.DisplayName, &sp.Description,
			&sp.BaseType, &sp.TypeConfig, &sp.IsArray, &sp.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, sp)
	}
	return result, nil
}

func (r *PGRepository) UpdateSharedProperty(ctx context.Context, sp *SharedProperty) error {
	typeConfig := sp.TypeConfig
	if len(typeConfig) == 0 {
		typeConfig = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE shared_properties SET display_name=$1, description=$2, base_type=$3,
		 type_config=$4, is_array=$5
		 WHERE rid=$6`,
		sp.DisplayName, sp.Description, sp.BaseType, typeConfig, sp.IsArray, sp.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteSharedProperty(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM shared_properties WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- TypeGroup ---

func (r *PGRepository) CreateTypeGroup(ctx context.Context, tg *TypeGroup) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO type_groups (rid, ontology_rid, api_name, display_name, description, color)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		tg.RID, tg.OntologyRID, tg.APIName, tg.DisplayName, tg.Description, tg.Color)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetTypeGroup(ctx context.Context, rid string) (*TypeGroup, error) {
	tg := &TypeGroup{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(color, ''), created_at
		 FROM type_groups WHERE rid = $1`, rid).
		Scan(&tg.RID, &tg.OntologyRID, &tg.APIName, &tg.DisplayName, &tg.Description,
			&tg.Color, &tg.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return tg, nil
}

func (r *PGRepository) ListTypeGroups(ctx context.Context, ontologyRID string) ([]TypeGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(color, ''), created_at
		 FROM type_groups
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TypeGroup
	for rows.Next() {
		var tg TypeGroup
		if err := rows.Scan(&tg.RID, &tg.OntologyRID, &tg.APIName, &tg.DisplayName, &tg.Description,
			&tg.Color, &tg.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, tg)
	}
	return result, nil
}

func (r *PGRepository) UpdateTypeGroup(ctx context.Context, tg *TypeGroup) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE type_groups SET display_name=$1, description=$2, color=$3
		 WHERE rid=$4`,
		tg.DisplayName, tg.Description, tg.Color, tg.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteTypeGroup(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM type_groups WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) AssignTypeGroup(ctx context.Context, objectTypeRID, typeGroupRID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO object_type_groups (object_type_rid, type_group_rid)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		objectTypeRID, typeGroupRID)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) RemoveTypeGroup(ctx context.Context, objectTypeRID, typeGroupRID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM object_type_groups WHERE object_type_rid = $1 AND type_group_rid = $2`,
		objectTypeRID, typeGroupRID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) ListTypeGroupsForObjectType(ctx context.Context, objectTypeRID string) ([]TypeGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tg.rid, tg.ontology_rid, tg.api_name, tg.display_name, COALESCE(tg.description, ''),
		 COALESCE(tg.color, ''), tg.created_at
		 FROM type_groups tg
		 INNER JOIN object_type_groups otg ON otg.type_group_rid = tg.rid
		 WHERE otg.object_type_rid = $1
		 ORDER BY tg.api_name`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TypeGroup
	for rows.Next() {
		var tg TypeGroup
		if err := rows.Scan(&tg.RID, &tg.OntologyRID, &tg.APIName, &tg.DisplayName, &tg.Description,
			&tg.Color, &tg.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, tg)
	}
	return result, nil
}

// --- ValueType ---

func (r *PGRepository) CreateValueType(ctx context.Context, vt *ValueType) error {
	constraints := vt.Constraints
	if len(constraints) == 0 {
		constraints = json.RawMessage(`{}`)
	}
	version := vt.Version
	if version == 0 {
		version = 1
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO value_types (rid, api_name, display_name, base_type, constraints, version)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		vt.RID, vt.APIName, vt.DisplayName, vt.BaseType, constraints, version)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetValueType(ctx context.Context, rid string) (*ValueType, error) {
	vt := &ValueType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, api_name, display_name, base_type, COALESCE(constraints, '{}'),
		 COALESCE(version, 1), created_at
		 FROM value_types WHERE rid = $1`, rid).
		Scan(&vt.RID, &vt.APIName, &vt.DisplayName, &vt.BaseType,
			&vt.Constraints, &vt.Version, &vt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return vt, nil
}

func (r *PGRepository) ListValueTypes(ctx context.Context) ([]ValueType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, api_name, display_name, base_type, COALESCE(constraints, '{}'),
		 COALESCE(version, 1), created_at
		 FROM value_types ORDER BY api_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ValueType
	for rows.Next() {
		var vt ValueType
		if err := rows.Scan(&vt.RID, &vt.APIName, &vt.DisplayName, &vt.BaseType,
			&vt.Constraints, &vt.Version, &vt.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, vt)
	}
	return result, nil
}

func (r *PGRepository) UpdateValueType(ctx context.Context, vt *ValueType) error {
	constraints := vt.Constraints
	if len(constraints) == 0 {
		constraints = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE value_types SET display_name=$1, base_type=$2, constraints=$3, version=$4
		 WHERE rid=$5`,
		vt.DisplayName, vt.BaseType, constraints, vt.Version, vt.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteValueType(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM value_types WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
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

// --- ActionLog ---

func (r *PGRepository) ListActionLogs(ctx context.Context, actionTypeRID string, limit, offset int) ([]ActionLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, action_type_rid, user_id, parameters, edits, status,
		 COALESCE(error_message, ''), created_at
		 FROM action_logs WHERE action_type_rid = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		actionTypeRID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ActionLog
	for rows.Next() {
		var al ActionLog
		if err := rows.Scan(&al.ID, &al.ActionTypeRID, &al.UserID, &al.Parameters,
			&al.Edits, &al.Status, &al.ErrorMessage, &al.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, al)
	}
	return result, nil
}

func (r *PGRepository) CountActionLogs(ctx context.Context, actionTypeRID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM action_logs WHERE action_type_rid = $1`,
		actionTypeRID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// --- Search ---

func (r *PGRepository) SearchOntologyResources(ctx context.Context, ontologyRID, query string) ([]SearchResult, error) {
	escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(query)
	pattern := "%" + escaped + "%"
	rows, err := r.pool.Query(ctx,
		`SELECT rid, resource_type, api_name, display_name, description, status FROM (
			SELECT rid, 'objectType' AS resource_type, api_name, display_name,
				COALESCE(description, '') AS description, COALESCE(status, 'ACTIVE') AS status
			FROM object_types WHERE (ontology_rid=$1 OR ontology_rid=(SELECT rid FROM ontologies WHERE api_name=$1 LIMIT 1)) AND (api_name ILIKE $2 OR display_name ILIKE $2)
			UNION ALL
			SELECT rid, 'linkType' AS resource_type, api_name, display_name,
				COALESCE(description, '') AS description, '' AS status
			FROM link_types WHERE (ontology_rid=$1 OR ontology_rid=(SELECT rid FROM ontologies WHERE api_name=$1 LIMIT 1)) AND (api_name ILIKE $2 OR display_name ILIKE $2)
			UNION ALL
			SELECT rid, 'actionType' AS resource_type, api_name, display_name,
				COALESCE(description, '') AS description, COALESCE(status, 'ACTIVE') AS status
			FROM action_types WHERE (ontology_rid=$1 OR ontology_rid=(SELECT rid FROM ontologies WHERE api_name=$1 LIMIT 1)) AND (api_name ILIKE $2 OR display_name ILIKE $2)
			UNION ALL
			SELECT rid, 'interface' AS resource_type, api_name, display_name,
				'' AS description, '' AS status
			FROM interfaces WHERE (ontology_rid=$1 OR ontology_rid=(SELECT rid FROM ontologies WHERE api_name=$1 LIMIT 1)) AND (api_name ILIKE $2 OR display_name ILIKE $2)
		) AS results ORDER BY api_name LIMIT 20`,
		ontologyRID, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SearchResult
	for rows.Next() {
		var sr SearchResult
		if err := rows.Scan(&sr.RID, &sr.ResourceType, &sr.APIName, &sr.DisplayName,
			&sr.Description, &sr.Status); err != nil {
			return nil, err
		}
		result = append(result, sr)
	}
	return result, nil
}

// --- Snapshot ---

func (r *PGRepository) CreateSnapshot(ctx context.Context, snapshot *OntologySnapshot) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO ontology_snapshots (ontology_rid, version, label, description, snapshot, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		snapshot.OntologyRID, snapshot.Version, snapshot.Label, snapshot.Description,
		snapshot.Snapshot, snapshot.CreatedBy).
		Scan(&snapshot.ID, &snapshot.CreatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListSnapshots(ctx context.Context, ontologyRID string) ([]OntologySnapshot, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, ontology_rid, version, COALESCE(label, ''), COALESCE(description, ''),
		 snapshot, COALESCE(created_by, 'system'), created_at
		 FROM ontology_snapshots
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY version DESC`,
		ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []OntologySnapshot
	for rows.Next() {
		var s OntologySnapshot
		if err := rows.Scan(&s.ID, &s.OntologyRID, &s.Version, &s.Label, &s.Description,
			&s.Snapshot, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func (r *PGRepository) GetSnapshot(ctx context.Context, ontologyRID string, version int) (*OntologySnapshot, error) {
	s := &OntologySnapshot{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, ontology_rid, version, COALESCE(label, ''), COALESCE(description, ''),
		 snapshot, COALESCE(created_by, 'system'), created_at
		 FROM ontology_snapshots
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND version = $2`,
		ontologyRID, version).
		Scan(&s.ID, &s.OntologyRID, &s.Version, &s.Label, &s.Description,
			&s.Snapshot, &s.CreatedBy, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *PGRepository) GetOntologyVersion(ctx context.Context, ontologyRID string) (int, error) {
	var version int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(current_version, 0) FROM ontologies WHERE rid = $1 OR api_name = $1`,
		ontologyRID).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return version, nil
}

func (r *PGRepository) IncrementOntologyVersion(ctx context.Context, ontologyRID string) (int, error) {
	var version int
	err := r.pool.QueryRow(ctx,
		`UPDATE ontologies SET current_version = COALESCE(current_version, 0) + 1
		 WHERE rid = $1 OR api_name = $1
		 RETURNING current_version`,
		ontologyRID).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return version, nil
}
