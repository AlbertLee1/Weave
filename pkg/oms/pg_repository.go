package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
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
		`SELECT rid, api_name, display_name, COALESCE(description, ''), COALESCE(current_version, 0), created_at, updated_at
		 FROM ontologies WHERE rid = $1 OR api_name = $1`, ridOrApiName).
		Scan(&o.RID, &o.APIName, &o.DisplayName, &o.Description, &o.CurrentVersion, &o.CreatedAt, &o.UpdatedAt)
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
		`SELECT rid, api_name, display_name, COALESCE(description, ''), COALESCE(current_version, 0), created_at, updated_at
		 FROM ontologies ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Ontology
	for rows.Next() {
		var o Ontology
		if err := rows.Scan(&o.RID, &o.APIName, &o.DisplayName, &o.Description, &o.CurrentVersion, &o.CreatedAt, &o.UpdatedAt); err != nil {
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
	pkProps := ot.EffectivePrimaryKeys()
	if pkProps == nil {
		pkProps = []string{}
	}
	pkPropsJSON, err := json.Marshal(pkProps)
	if err != nil {
		return fmt.Errorf("encode primaryKeys: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO object_types (rid, ontology_rid, api_name, display_name, plural_display_name,
		 description, primary_key_prop, title_property, status, visibility, icon_name, color,
		 deprecated_reason, deprecated_deadline, primary_key_props, extends_rid, classification, audit_data_access,
		 is_event, event_start_prop, event_end_prop)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NULLIF($16, ''), NULLIF($17, ''), $18,
		 $19, NULLIF($20, ''), NULLIF($21, ''))`,
		ot.RID, ot.OntologyRID, ot.APIName, ot.DisplayName, ot.PluralDisplayName,
		ot.Description, ot.PrimaryKey, ot.TitleProperty, ot.Status, ot.Visibility,
		ot.IconName, ot.Color, ot.DeprecatedReason, ot.DeprecatedDeadline, pkPropsJSON, ot.ExtendsRID, ot.Classification, ot.AuditDataAccess,
		ot.IsEvent, ot.EventStartProp, ot.EventEndProp)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetObjectType(ctx context.Context, rid string) (*ObjectType, error) {
	ot := &ObjectType{}
	var pkPropsJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(plural_display_name, ''),
		 COALESCE(description, ''), primary_key_prop, COALESCE(title_property, ''),
		 COALESCE(status, 'ACTIVE'), COALESCE(visibility, 'NORMAL'),
		 COALESCE(icon_name, ''), COALESCE(color, ''),
		 COALESCE(deprecated_reason, ''), deprecated_deadline,
		 created_at, updated_at, COALESCE(primary_key_props, '[]'::jsonb),
		 COALESCE(extends_rid, ''), COALESCE(classification, ''), COALESCE(audit_data_access, false),
		 COALESCE(is_event, false), COALESCE(event_start_prop, ''), COALESCE(event_end_prop, '')
		 FROM object_types WHERE rid = $1`, rid).
		Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.DeprecatedReason, &ot.DeprecatedDeadline,
			&ot.CreatedAt, &ot.UpdatedAt, &pkPropsJSON, &ot.ExtendsRID, &ot.Classification, &ot.AuditDataAccess,
			&ot.IsEvent, &ot.EventStartProp, &ot.EventEndProp)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ot.PrimaryKeys = decodePrimaryKeyProps(pkPropsJSON, ot.PrimaryKey)

	// Load properties
	props, err := r.ListProperties(ctx, ot.RID)
	if err != nil {
		return nil, err
	}
	ot.Properties = props

	return ot, nil
}

// decodePrimaryKeyProps unmarshals the JSONB primary_key_props column. Empty
// or malformed columns fall back to a single-element list over the legacy
// primary_key_prop column so rows from before migration 000037 (or rows
// where the JSONB column was set to '[]') still expose a usable key list.
func decodePrimaryKeyProps(raw []byte, legacy string) []string {
	if len(raw) > 0 {
		var pks []string
		if err := json.Unmarshal(raw, &pks); err == nil && len(pks) > 0 {
			return pks
		}
	}
	if legacy != "" {
		return []string{legacy}
	}
	return nil
}

func (r *PGRepository) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error) {
	var rid string
	err := r.pool.QueryRow(ctx,
		`SELECT rid FROM object_types
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND (rid = $2 OR api_name = $2)`,
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
		 created_at, updated_at, COALESCE(primary_key_props, '[]'::jsonb),
		 COALESCE(extends_rid, ''), COALESCE(classification, ''), COALESCE(audit_data_access, false),
		 COALESCE(is_event, false), COALESCE(event_start_prop, ''), COALESCE(event_end_prop, '')
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
		var pkPropsJSON []byte
		if err := rows.Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.DeprecatedReason, &ot.DeprecatedDeadline,
			&ot.CreatedAt, &ot.UpdatedAt, &pkPropsJSON, &ot.ExtendsRID, &ot.Classification, &ot.AuditDataAccess,
			&ot.IsEvent, &ot.EventStartProp, &ot.EventEndProp); err != nil {
			return nil, err
		}
		ot.PrimaryKeys = decodePrimaryKeyProps(pkPropsJSON, ot.PrimaryKey)
		result = append(result, ot)
	}
	return result, nil
}

func (r *PGRepository) UpdateObjectType(ctx context.Context, ot *ObjectType) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE object_types SET display_name=$1, plural_display_name=$2, description=$3,
		 title_property=$4, status=$5, visibility=$6, icon_name=$7, color=$8,
		 deprecated_reason=$9, deprecated_deadline=$10, extends_rid=NULLIF($11, ''),
		 classification=NULLIF($12, ''), audit_data_access=$13,
		 is_event=$14, event_start_prop=NULLIF($15, ''), event_end_prop=NULLIF($16, ''),
		 updated_at=now()
		 WHERE rid=$17`,
		ot.DisplayName, ot.PluralDisplayName, ot.Description,
		ot.TitleProperty, ot.Status, ot.Visibility, ot.IconName, ot.Color,
		ot.DeprecatedReason, ot.DeprecatedDeadline, ot.ExtendsRID, ot.Classification, ot.AuditDataAccess,
		ot.IsEvent, ot.EventStartProp, ot.EventEndProp, ot.RID)
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
		 base_type, type_config, is_array, is_nullable, is_searchable, is_sortable, status, shared_property_rid, is_edit_only,
		 derived, formula, classification)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NULLIF($17, ''))`,
		p.RID, p.ObjectTypeRID, p.APIName, p.DisplayName, p.Description,
		p.BaseType, typeConfig, p.IsArray, p.IsNullable, p.IsSearchable, p.IsSortable, status, nilIfEmpty(p.SharedPropertyRID), p.IsEditOnly,
		p.Derived, p.Formula, p.Classification)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, api_name, COALESCE(display_name, ''), COALESCE(description, ''),
		 base_type, COALESCE(type_config, '{}'), is_array, is_nullable, is_searchable, is_sortable,
		 COALESCE(status, 'ACTIVE'), COALESCE(deprecated_reason, ''), COALESCE(shared_property_rid, ''), is_edit_only,
		 COALESCE(derived, false), COALESCE(formula, ''), COALESCE(classification, ''), created_at
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
			&p.Status, &p.DeprecatedReason, &p.SharedPropertyRID, &p.IsEditOnly,
			&p.Derived, &p.Formula, &p.Classification, &p.CreatedAt); err != nil {
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
		 COALESCE(status, 'ACTIVE'), COALESCE(deprecated_reason, ''), COALESCE(shared_property_rid, ''), is_edit_only,
		 COALESCE(derived, false), COALESCE(formula, ''), COALESCE(classification, ''), created_at
		 FROM properties WHERE rid = $1`, rid).
		Scan(&p.RID, &p.ObjectTypeRID, &p.APIName, &p.DisplayName, &p.Description,
			&p.BaseType, &p.TypeConfig, &p.IsArray, &p.IsNullable, &p.IsSearchable, &p.IsSortable,
			&p.Status, &p.DeprecatedReason, &p.SharedPropertyRID, &p.IsEditOnly,
			&p.Derived, &p.Formula, &p.Classification, &p.CreatedAt)
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
		 status=$6, deprecated_reason=$7, is_edit_only=$8,
		 derived=$9, formula=$10, classification=NULLIF($11, '')
		 WHERE rid=$12`,
		p.DisplayName, p.Description, p.IsSearchable, p.IsSortable, p.IsNullable,
		p.Status, p.DeprecatedReason, p.IsEditOnly, p.Derived, p.Formula, p.Classification, p.RID)
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
	typeClasses := lt.TypeClasses
	if typeClasses == nil {
		typeClasses = []string{}
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO link_types (rid, ontology_rid, api_name, display_name, description,
		 source_object_type, target_object_type, cardinality, foreign_key_config, join_table_config, is_required, inverse_link_rid, propagate_markings, type_classes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), $13, $14)`,
		lt.RID, lt.OntologyRID, lt.APIName, lt.DisplayName, lt.Description,
		lt.SourceObjectType, lt.TargetObjectType, lt.Cardinality,
		fkConfig, jtConfig, lt.IsRequired, lt.InverseLinkRID, lt.PropagateMarkings, typeClasses)
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
		 foreign_key_config, join_table_config, is_required,
		 COALESCE(inverse_link_rid, ''), propagate_markings, COALESCE(type_classes, '{}'::text[]), created_at
		 FROM link_types WHERE rid = $1`, rid).
		Scan(&lt.RID, &lt.OntologyRID, &lt.APIName, &lt.DisplayName, &lt.Description,
			&lt.SourceObjectType, &lt.TargetObjectType, &lt.Cardinality,
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired,
			&lt.InverseLinkRID, &lt.PropagateMarkings, &lt.TypeClasses, &lt.CreatedAt)
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
		 foreign_key_config, join_table_config, is_required,
		 COALESCE(inverse_link_rid, ''), propagate_markings, COALESCE(type_classes, '{}'::text[]), created_at
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
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired,
			&lt.InverseLinkRID, &lt.PropagateMarkings, &lt.TypeClasses, &lt.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, lt)
	}
	return result, nil
}

// ListIncomingLinkTypes returns link types whose target is the given object type.
// Used by reverse traversal to discover which link types can land at a given type.
func (r *PGRepository) ListIncomingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 source_object_type, target_object_type, cardinality,
		 foreign_key_config, join_table_config, is_required,
		 COALESCE(inverse_link_rid, ''), propagate_markings, COALESCE(type_classes, '{}'::text[]), created_at
		 FROM link_types WHERE target_object_type = $1 ORDER BY api_name`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []LinkType
	for rows.Next() {
		var lt LinkType
		if err := rows.Scan(&lt.RID, &lt.OntologyRID, &lt.APIName, &lt.DisplayName, &lt.Description,
			&lt.SourceObjectType, &lt.TargetObjectType, &lt.Cardinality,
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired,
			&lt.InverseLinkRID, &lt.PropagateMarkings, &lt.TypeClasses, &lt.CreatedAt); err != nil {
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
		 foreign_key_config, join_table_config, is_required,
		 COALESCE(inverse_link_rid, ''), propagate_markings, COALESCE(type_classes, '{}'::text[]), created_at
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
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired,
			&lt.InverseLinkRID, &lt.PropagateMarkings, &lt.TypeClasses, &lt.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, lt)
	}
	return result, nil
}

func (r *PGRepository) UpdateLinkType(ctx context.Context, lt *LinkType) error {
	typeClasses := lt.TypeClasses
	if typeClasses == nil {
		typeClasses = []string{}
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE link_types SET display_name=$1, description=$2, is_required=$3,
		 inverse_link_rid=NULLIF($4, ''), propagate_markings=$5, type_classes=$6
		 WHERE rid=$7`,
		lt.DisplayName, lt.Description, lt.IsRequired, lt.InverseLinkRID, lt.PropagateMarkings, typeClasses, lt.RID)
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
	approvers, err := encodeApprovers(at.Approvers)
	if err != nil {
		return err
	}
	paramSchema := normaliseParameterSchemaForWrite(at.ParameterSchema)
	branchID := NormalizeBranchID(at.BranchID)
	_, err = r.pool.Exec(ctx,
		`INSERT INTO action_types (rid, ontology_rid, api_name, display_name, description,
		 status, parameters, rules, function_rid, is_function_backed, submission_criteria, side_effects,
		 implements_method_rid, compensate_action_rid, requires_approval, approvers, parameter_schema, function_version, branch_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''), NULLIF($14, ''), $15, $16, $17, $18, $19)`,
		at.RID, at.OntologyRID, at.APIName, at.DisplayName, at.Description,
		at.Status, params, rules, at.FunctionRID, at.IsFunctionBacked, sc, se,
		at.ImplementsMethodRID, at.CompensateActionRID,
		at.RequiresApproval, approvers, paramSchema, at.FunctionVersion, branchID)
	if err != nil {
		return wrapPGError(err)
	}
	at.BranchID = branchID
	return nil
}

func (r *PGRepository) GetActionType(ctx context.Context, rid string) (*ActionType, error) {
	at := &ActionType{}
	var approvers []byte
	var paramSchema []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(status, 'ACTIVE'), parameters, rules,
		 COALESCE(function_rid, ''), is_function_backed, created_at,
		 submission_criteria, side_effects,
		 COALESCE(implements_method_rid, ''),
		 COALESCE(compensate_action_rid, ''),
		 COALESCE(requires_approval, FALSE),
		 COALESCE(approvers, '[]'::jsonb),
		 parameter_schema,
		 COALESCE(function_version, ''),
		 COALESCE(branch_id, 'main')
		 FROM action_types WHERE rid = $1`, rid).
		Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt,
			&at.SubmissionCriteria, &at.SideEffects,
			&at.ImplementsMethodRID,
			&at.CompensateActionRID,
			&at.RequiresApproval,
			&approvers,
			&paramSchema,
			&at.FunctionVersion,
			&at.BranchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	at.Approvers = decodeApprovers(approvers)
	at.ParameterSchema = parameterSchemaFromBytes(paramSchema)
	return at, nil
}

func (r *PGRepository) GetActionTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*ActionType, error) {
	branch := BranchScopeFromContext(ctx)
	var rid string
	// Prefer the row stamped with the requested branch; fall back to the
	// main row when the branch has no override. The CASE expression
	// orders the candidates so the branch-specific row sorts first.
	err := r.pool.QueryRow(ctx,
		`SELECT rid FROM action_types
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND (rid = $2 OR api_name = $2)
		 AND branch_id IN ($3, 'main')
		 ORDER BY (CASE WHEN branch_id = $3 THEN 0 ELSE 1 END)
		 LIMIT 1`,
		ontologyRID, apiNameOrRID, branch).Scan(&rid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r.GetActionType(ctx, rid)
}

func (r *PGRepository) ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error) {
	branch := BranchScopeFromContext(ctx)
	// DISTINCT ON (api_name) keeps a single row per ApiName, preferring
	// the branch-specific row over the main fallback. The ORDER BY clause
	// must mirror the DISTINCT ON column list and tie-break by the branch
	// preference so PostgreSQL's "first row wins" semantics pick the
	// branch overlay when present (US-384).
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (api_name)
		 rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 COALESCE(status, 'ACTIVE'), parameters, rules,
		 COALESCE(function_rid, ''), is_function_backed, created_at,
		 submission_criteria, side_effects,
		 COALESCE(implements_method_rid, ''),
		 COALESCE(compensate_action_rid, ''),
		 COALESCE(requires_approval, FALSE),
		 COALESCE(approvers, '[]'::jsonb),
		 parameter_schema,
		 COALESCE(function_version, ''),
		 COALESCE(branch_id, 'main')
		 FROM action_types
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND branch_id IN ($2, 'main')
		 ORDER BY api_name, (CASE WHEN branch_id = $2 THEN 0 ELSE 1 END)`,
		ontologyRID, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ActionType
	for rows.Next() {
		var at ActionType
		var approvers []byte
		var paramSchema []byte
		if err := rows.Scan(&at.RID, &at.OntologyRID, &at.APIName, &at.DisplayName, &at.Description,
			&at.Status, &at.Parameters, &at.Rules,
			&at.FunctionRID, &at.IsFunctionBacked, &at.CreatedAt,
			&at.SubmissionCriteria, &at.SideEffects,
			&at.ImplementsMethodRID,
			&at.CompensateActionRID,
			&at.RequiresApproval,
			&approvers,
			&paramSchema,
			&at.FunctionVersion,
			&at.BranchID); err != nil {
			return nil, err
		}
		at.Approvers = decodeApprovers(approvers)
		at.ParameterSchema = parameterSchemaFromBytes(paramSchema)
		result = append(result, at)
	}
	return result, nil
}

func (r *PGRepository) UpdateActionType(ctx context.Context, at *ActionType) error {
	approvers, err := encodeApprovers(at.Approvers)
	if err != nil {
		return err
	}
	paramSchema := normaliseParameterSchemaForWrite(at.ParameterSchema)
	tag, err := r.pool.Exec(ctx,
		`UPDATE action_types SET display_name=$1, description=$2, status=$3,
		 parameters=$4, rules=$5, submission_criteria=$6, side_effects=$7,
		 implements_method_rid=NULLIF($8, ''),
		 compensate_action_rid=NULLIF($9, ''),
		 requires_approval=$10, approvers=$11, parameter_schema=$12,
		 function_version=$13 WHERE rid=$14`,
		at.DisplayName, at.Description, at.Status, at.Parameters, at.Rules,
		at.SubmissionCriteria, at.SideEffects, at.ImplementsMethodRID,
		at.CompensateActionRID, at.RequiresApproval, approvers, paramSchema,
		at.FunctionVersion, at.RID)
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

// encodeApprovers marshals the Approvers slice for the action_types.approvers
// JSONB column. Nil / empty is rendered as '[]' so the NOT NULL DEFAULT holds.
func encodeApprovers(approvers []string) ([]byte, error) {
	if len(approvers) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(approvers)
}

// decodeApprovers decodes the action_types.approvers JSONB bytes into a
// []string. Legacy rows (pre-US-242) have an empty or NULL column which
// COALESCE turns into '[]'; either way we return nil so callers can rely on
// len(Approvers) == 0 for the "no gating" check.
func decodeApprovers(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normaliseParameterSchemaForWrite maps the in-memory ParameterSchema blob
// onto a value pgx can hand to the nullable JSONB column. A nil / empty / JSON
// "null" raw message becomes a true SQL NULL so GetActionType round-trips
// "no schema" as nil. pgx encodes nil json.RawMessage as the literal string
// "null", which the JSONB column accepts but breaks the empty-round-trip
// contract — so we normalise here at the single write choke point.
func normaliseParameterSchemaForWrite(raw json.RawMessage) interface{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}

// parameterSchemaFromBytes decodes the action_types.parameter_schema JSONB
// bytes into a json.RawMessage; NULL rows (legacy / pre-US-245) and the
// literal JSON "null" both decode to nil so callers can rely on len == 0
// for the "no schema declared" check.
func parameterSchemaFromBytes(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

// hasParameterSchemaRaw reports whether a wire-inbound ParameterSchema blob
// carries a non-empty, non-null JSON Schema. Handler layer uses this to
// distinguish "keep as-is" (request omits the field) from "clear" (request
// sends null / empty) on PATCH-shaped action-type updates.
func hasParameterSchemaRaw(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

// --- Interface ---

func (r *PGRepository) CreateInterface(ctx context.Context, iface *Interface) error {
	sharedProps := iface.SharedProperties
	if len(sharedProps) == 0 {
		sharedProps = json.RawMessage(`[]`)
	}
	outgoingLT := iface.OutgoingLinkTypes
	if len(outgoingLT) == 0 {
		outgoingLT = json.RawMessage(`[]`)
	}
	var extendsRID *string
	if iface.ExtendsRID != "" {
		extendsRID = &iface.ExtendsRID
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO interfaces (rid, ontology_rid, api_name, display_name, extends_rid, shared_properties, outgoing_link_types)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		iface.RID, iface.OntologyRID, iface.APIName, iface.DisplayName, extendsRID, sharedProps, outgoingLT)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetInterface(ctx context.Context, rid string) (*Interface, error) {
	i := &Interface{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(extends_rid, ''),
		 COALESCE(shared_properties, '[]'), COALESCE(outgoing_link_types, '[]'), created_at
		 FROM interfaces WHERE rid = $1`, rid).
		Scan(&i.RID, &i.OntologyRID, &i.APIName, &i.DisplayName,
			&i.ExtendsRID, &i.SharedProperties, &i.OutgoingLinkTypes, &i.CreatedAt)
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
		 COALESCE(shared_properties, '[]'), COALESCE(outgoing_link_types, '[]'), created_at
		 FROM interfaces
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND (rid = $2 OR api_name = $2)`, ontologyRID, apiName).
		Scan(&i.RID, &i.OntologyRID, &i.APIName, &i.DisplayName,
			&i.ExtendsRID, &i.SharedProperties, &i.OutgoingLinkTypes, &i.CreatedAt)
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
		 COALESCE(shared_properties, '[]'), COALESCE(outgoing_link_types, '[]'), created_at
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
			&i.ExtendsRID, &i.SharedProperties, &i.OutgoingLinkTypes, &i.CreatedAt); err != nil {
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
	outgoingLT := iface.OutgoingLinkTypes
	if len(outgoingLT) == 0 {
		outgoingLT = json.RawMessage(`[]`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE interfaces SET display_name=$1, extends_rid=$2, shared_properties=$3, outgoing_link_types=$4
		 WHERE rid=$5`,
		iface.DisplayName, extendsRID, sharedProps, outgoingLT, iface.RID)
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
		 COALESCE(ot.description, ''), ot.primary_key_prop, COALESCE(ot.title_property, ''),
		 COALESCE(ot.status, 'ACTIVE'), COALESCE(ot.visibility, 'NORMAL'),
		 COALESCE(ot.icon_name, ''), COALESCE(ot.color, ''),
		 COALESCE(ot.deprecated_reason, ''), ot.deprecated_deadline,
		 ot.created_at, ot.updated_at, COALESCE(ot.primary_key_props, '[]'::jsonb),
		 COALESCE(ot.extends_rid, ''), COALESCE(ot.classification, ''), COALESCE(ot.audit_data_access, false),
		 COALESCE(ot.is_event, false), COALESCE(ot.event_start_prop, ''), COALESCE(ot.event_end_prop, '')
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
		var pkPropsJSON []byte
		if err := rows.Scan(&ot.RID, &ot.OntologyRID, &ot.APIName, &ot.DisplayName, &ot.PluralDisplayName,
			&ot.Description, &ot.PrimaryKey, &ot.TitleProperty,
			&ot.Status, &ot.Visibility, &ot.IconName, &ot.Color,
			&ot.DeprecatedReason, &ot.DeprecatedDeadline,
			&ot.CreatedAt, &ot.UpdatedAt, &pkPropsJSON, &ot.ExtendsRID, &ot.Classification, &ot.AuditDataAccess,
			&ot.IsEvent, &ot.EventStartProp, &ot.EventEndProp); err != nil {
			return nil, err
		}
		ot.PrimaryKeys = decodePrimaryKeyProps(pkPropsJSON, ot.PrimaryKey)
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

func (r *PGRepository) GetValueTypeByAPIName(ctx context.Context, ridOrApiName string) (*ValueType, error) {
	vt := &ValueType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, api_name, display_name, base_type, COALESCE(constraints, '{}'),
		 COALESCE(version, 1), created_at
		 FROM value_types WHERE (rid = $1 OR api_name = $1)`, ridOrApiName).
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

// ListPropertyUsagesByBaseType returns every Property+ObjectType pair whose
// Property.base_type equals the supplied apiName. The join is keyed on
// properties.object_type_rid → object_types.rid; ordering matches the UI's
// expected display ordering (ObjectType apiName first, then Property
// apiName) so the wire shape is stable without client-side resorting.
func (r *PGRepository) ListPropertyUsagesByBaseType(ctx context.Context, baseType string) ([]PropertyUsage, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.rid, p.api_name, ot.rid, ot.api_name
		 FROM properties p
		 JOIN object_types ot ON ot.rid = p.object_type_rid
		 WHERE p.base_type = $1
		 ORDER BY ot.api_name, p.api_name`, baseType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PropertyUsage
	for rows.Next() {
		var u PropertyUsage
		if err := rows.Scan(&u.PropertyRID, &u.PropertyAPIName, &u.ObjectTypeRID, &u.ObjectTypeAPIName); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, nil
}

// wrapPGError maps common PG errors to domain errors.
func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

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

// --- SecurityPolicy ---

func (r *PGRepository) CreateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error {
	rules := sp.Rules
	if len(rules) == 0 {
		rules = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO security_policies (rid, object_type_rid, policy_type, rules)
		 VALUES ($1, $2, $3, $4)`,
		sp.RID, sp.ObjectTypeRID, sp.PolicyType, rules)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetSecurityPolicy(ctx context.Context, rid string) (*SecurityPolicy, error) {
	sp := &SecurityPolicy{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, object_type_rid, policy_type, rules, created_at
		 FROM security_policies WHERE rid = $1`, rid).
		Scan(&sp.RID, &sp.ObjectTypeRID, &sp.PolicyType, &sp.Rules, &sp.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sp, nil
}

func (r *PGRepository) ListSecurityPolicies(ctx context.Context, objectTypeRID string) ([]SecurityPolicy, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, policy_type, rules, created_at
		 FROM security_policies WHERE object_type_rid = $1
		 ORDER BY created_at`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SecurityPolicy
	for rows.Next() {
		var sp SecurityPolicy
		if err := rows.Scan(&sp.RID, &sp.ObjectTypeRID, &sp.PolicyType, &sp.Rules, &sp.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, sp)
	}
	return result, nil
}

func (r *PGRepository) UpdateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error {
	rules := sp.Rules
	if len(rules) == 0 {
		rules = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE security_policies SET policy_type=$1, rules=$2
		 WHERE rid=$3`,
		sp.PolicyType, rules, sp.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteSecurityPolicy(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM security_policies WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- DatasourceBinding ---

func (r *PGRepository) CreateDatasourceBinding(ctx context.Context, db *DatasourceBinding) error {
	colMapping := db.ColumnMapping
	if len(colMapping) == 0 {
		colMapping = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO datasource_bindings (rid, object_type_rid, dataset_rid, branch, column_mapping, is_primary)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		db.RID, db.ObjectTypeRID, db.DatasetRID, db.Branch, colMapping, db.IsPrimary)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetDatasourceBinding(ctx context.Context, rid string) (*DatasourceBinding, error) {
	db := &DatasourceBinding{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, object_type_rid, dataset_rid, COALESCE(branch, 'main'), column_mapping, is_primary, created_at
		 FROM datasource_bindings WHERE rid = $1`, rid).
		Scan(&db.RID, &db.ObjectTypeRID, &db.DatasetRID, &db.Branch, &db.ColumnMapping, &db.IsPrimary, &db.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return db, nil
}

func (r *PGRepository) ListDatasourceBindings(ctx context.Context, objectTypeRID string) ([]DatasourceBinding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, object_type_rid, dataset_rid, COALESCE(branch, 'main'), column_mapping, is_primary, created_at
		 FROM datasource_bindings WHERE object_type_rid = $1 ORDER BY created_at`, objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DatasourceBinding
	for rows.Next() {
		var db DatasourceBinding
		if err := rows.Scan(&db.RID, &db.ObjectTypeRID, &db.DatasetRID, &db.Branch,
			&db.ColumnMapping, &db.IsPrimary, &db.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, db)
	}
	return result, nil
}

func (r *PGRepository) UpdateDatasourceBinding(ctx context.Context, db *DatasourceBinding) error {
	colMapping := db.ColumnMapping
	if len(colMapping) == 0 {
		colMapping = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE datasource_bindings SET dataset_rid=$1, branch=$2, column_mapping=$3, is_primary=$4
		 WHERE rid=$5`,
		db.DatasetRID, db.Branch, colMapping, db.IsPrimary, db.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteDatasourceBinding(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM datasource_bindings WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- QueryType ---

func (r *PGRepository) CreateQueryType(ctx context.Context, qt *QueryType) error {
	params := qt.Parameters
	if len(params) == 0 {
		params = json.RawMessage(`[]`)
	}
	output := qt.Output
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	query := qt.Query
	if len(query) == 0 {
		query = json.RawMessage(`{}`)
	}
	status := qt.Status
	if status == "" {
		status = "ACTIVE"
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO query_types (rid, ontology_rid, api_name, display_name, description, parameters, output, query, status, function_rid)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		qt.RID, qt.OntologyRID, qt.APIName, qt.DisplayName, qt.Description,
		params, output, query, status, qt.FunctionRID)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetQueryType(ctx context.Context, rid string) (*QueryType, error) {
	qt := &QueryType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 parameters, output, query, COALESCE(status, 'ACTIVE'), COALESCE(function_rid, ''), created_at
		 FROM query_types WHERE rid = $1`, rid).
		Scan(&qt.RID, &qt.OntologyRID, &qt.APIName, &qt.DisplayName, &qt.Description,
			&qt.Parameters, &qt.Output, &qt.Query, &qt.Status, &qt.FunctionRID, &qt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return qt, nil
}

func (r *PGRepository) GetQueryTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*QueryType, error) {
	qt := &QueryType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 parameters, output, query, COALESCE(status, 'ACTIVE'), COALESCE(function_rid, ''), created_at
		 FROM query_types
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND (rid = $2 OR api_name = $2)`, ontologyRID, apiName).
		Scan(&qt.RID, &qt.OntologyRID, &qt.APIName, &qt.DisplayName, &qt.Description,
			&qt.Parameters, &qt.Output, &qt.Query, &qt.Status, &qt.FunctionRID, &qt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return qt, nil
}

func (r *PGRepository) ListQueryTypes(ctx context.Context, ontologyRID string) ([]QueryType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 parameters, output, query, COALESCE(status, 'ACTIVE'), COALESCE(function_rid, ''), created_at
		 FROM query_types
		 WHERE ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1)
		 ORDER BY api_name`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []QueryType
	for rows.Next() {
		var qt QueryType
		if err := rows.Scan(&qt.RID, &qt.OntologyRID, &qt.APIName, &qt.DisplayName, &qt.Description,
			&qt.Parameters, &qt.Output, &qt.Query, &qt.Status, &qt.FunctionRID, &qt.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, qt)
	}
	return result, nil
}

func (r *PGRepository) UpdateQueryType(ctx context.Context, qt *QueryType) error {
	params := qt.Parameters
	if len(params) == 0 {
		params = json.RawMessage(`[]`)
	}
	output := qt.Output
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	query := qt.Query
	if len(query) == 0 {
		query = json.RawMessage(`{}`)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE query_types SET display_name=$1, description=$2, parameters=$3, output=$4, query=$5, status=$6, function_rid=$7
		 WHERE rid=$8`,
		qt.DisplayName, qt.Description, params, output, query, qt.Status, qt.FunctionRID, qt.RID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteQueryType(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM query_types WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Function ---

func (r *PGRepository) CreateFunction(ctx context.Context, fn *Function) error {
	signature := normaliseSignatureForWrite(fn.Signature)
	runtime := fn.NormalisedRuntime()
	version := fn.NormalisedVersion()
	codeHash := HashFunctionCode(fn.SourceCode)
	sigHash := HashFunctionSignature(signature)
	branchID := NormalizeBranchID(fn.BranchID)
	dependsOn := normaliseDependsOnForWrite(fn.DependsOn)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO functions (rid, ontology_rid, name, version, source_code, created_by, signature, runtime, pure, code_hash, signature_hash, published_at, branch_id, depends_on)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), $12, $13)`,
		fn.RID, fn.OntologyRID, fn.Name, version, fn.SourceCode, fn.CreatedBy, signature, runtime, fn.Pure, codeHash, sigHash, branchID, dependsOn)
	if err != nil {
		return wrapPGError(err)
	}
	fn.Runtime = runtime
	fn.Version = version
	fn.CodeHash = codeHash
	fn.SignatureHash = sigHash
	fn.BranchID = branchID
	fn.DependsOn = dependsOn
	if len(signature) > 0 {
		fn.Signature = signature
	}
	return nil
}

func (r *PGRepository) GetFunction(ctx context.Context, rid string) (*Function, error) {
	fn := &Function{}
	var sig []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, name, version, source_code, COALESCE(created_by, ''),
		        COALESCE(signature, '{}'::jsonb), COALESCE(runtime, 'goja'), COALESCE(pure, FALSE), created_at,
		        COALESCE(code_hash, ''), COALESCE(signature_hash, ''), COALESCE(published_at, created_at),
		        COALESCE(branch_id, 'main'), COALESCE(depends_on, '{}'::text[])
		 FROM functions WHERE rid = $1`, rid).
		Scan(&fn.RID, &fn.OntologyRID, &fn.Name, &fn.Version, &fn.SourceCode, &fn.CreatedBy,
			&sig, &fn.Runtime, &fn.Pure, &fn.CreatedAt,
			&fn.CodeHash, &fn.SignatureHash, &fn.PublishedAt, &fn.BranchID, &fn.DependsOn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	fn.Signature = signatureFromBytes(sig)
	if fn.CodeHash == "" {
		fn.CodeHash = HashFunctionCode(fn.SourceCode)
	}
	if fn.SignatureHash == "" {
		fn.SignatureHash = HashFunctionSignature(fn.Signature)
	}
	return fn, nil
}

// GetFunctionByName resolves a function by RID or by name within the given
// ontology. When multiple versions exist for the same name (US-217), this
// returns the latest semver — callers wanting a specific version should use
// GetFunctionByNameVersion or pass the URL segment through ResolveFunctionRef.
func (r *PGRepository) GetFunctionByName(ctx context.Context, ontologyRID, name string) (*Function, error) {
	branch := BranchScopeFromContext(ctx)
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, name, version, source_code, COALESCE(created_by, ''),
		        COALESCE(signature, '{}'::jsonb), COALESCE(runtime, 'goja'), COALESCE(pure, FALSE), created_at,
		        COALESCE(code_hash, ''), COALESCE(signature_hash, ''), COALESCE(published_at, created_at),
		        COALESCE(branch_id, 'main'), COALESCE(depends_on, '{}'::text[])
		 FROM functions
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND (rid = $2 OR name = $2)
		 AND branch_id IN ($3, 'main')`, ontologyRID, name, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates, err := scanFunctions(rows)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNotFound
	}
	candidates = preferBranchFunctions(candidates, branch)
	SortFunctionsByVersionDesc(candidates)
	winner := candidates[0]
	return &winner, nil
}

// GetFunctionByNameVersion resolves a function row pinned to a specific
// semver. Used by URL refs of the form `name@version`.
func (r *PGRepository) GetFunctionByNameVersion(ctx context.Context, ontologyRID, name, version string) (*Function, error) {
	branch := BranchScopeFromContext(ctx)
	fn := &Function{}
	var sig []byte
	// Prefer branch-specific row, fall back to main, like
	// GetActionTypeByAPIName. The CASE expression orders the candidates
	// so the branch-specific (name, version) row sorts first.
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, name, version, source_code, COALESCE(created_by, ''),
		        COALESCE(signature, '{}'::jsonb), COALESCE(runtime, 'goja'), COALESCE(pure, FALSE), created_at,
		        COALESCE(code_hash, ''), COALESCE(signature_hash, ''), COALESCE(published_at, created_at),
		        COALESCE(branch_id, 'main'), COALESCE(depends_on, '{}'::text[])
		 FROM functions
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND name = $2 AND version = $3
		 AND branch_id IN ($4, 'main')
		 ORDER BY (CASE WHEN branch_id = $4 THEN 0 ELSE 1 END)
		 LIMIT 1`, ontologyRID, name, version, branch).
		Scan(&fn.RID, &fn.OntologyRID, &fn.Name, &fn.Version, &fn.SourceCode, &fn.CreatedBy,
			&sig, &fn.Runtime, &fn.Pure, &fn.CreatedAt,
			&fn.CodeHash, &fn.SignatureHash, &fn.PublishedAt, &fn.BranchID, &fn.DependsOn)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	fn.Signature = signatureFromBytes(sig)
	if fn.CodeHash == "" {
		fn.CodeHash = HashFunctionCode(fn.SourceCode)
	}
	if fn.SignatureHash == "" {
		fn.SignatureHash = HashFunctionSignature(fn.Signature)
	}
	return fn, nil
}

// ListFunctionVersionsByName returns every stored version of the named
// function within the ontology, sorted latest-first via
// SortFunctionsByVersionDesc. When the request context carries a non-main
// branch scope, branch-specific versions shadow the main-branch versions
// they share a semver with — but main-only versions stay visible so the
// branch inherits the trunk's history (US-384).
func (r *PGRepository) ListFunctionVersionsByName(ctx context.Context, ontologyRID, name string) ([]Function, error) {
	branch := BranchScopeFromContext(ctx)
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, name, version, source_code, COALESCE(created_by, ''),
		        COALESCE(signature, '{}'::jsonb), COALESCE(runtime, 'goja'), COALESCE(pure, FALSE), created_at,
		        COALESCE(code_hash, ''), COALESCE(signature_hash, ''), COALESCE(published_at, created_at),
		        COALESCE(branch_id, 'main'), COALESCE(depends_on, '{}'::text[])
		 FROM functions
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND name = $2
		 AND branch_id IN ($3, 'main')`, ontologyRID, name, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanFunctions(rows)
	if err != nil {
		return nil, err
	}
	out = preferBranchFunctions(out, branch)
	SortFunctionsByVersionDesc(out)
	return out, nil
}

// scanFunctions iterates a pgx rows handle that selects the canonical
// function column list. Centralised so the per-row signature/runtime decoding
// stays in one place across the read paths.
func scanFunctions(rows pgx.Rows) ([]Function, error) {
	var out []Function
	for rows.Next() {
		var fn Function
		var sig []byte
		if err := rows.Scan(&fn.RID, &fn.OntologyRID, &fn.Name, &fn.Version, &fn.SourceCode,
			&fn.CreatedBy, &sig, &fn.Runtime, &fn.Pure, &fn.CreatedAt,
			&fn.CodeHash, &fn.SignatureHash, &fn.PublishedAt, &fn.BranchID, &fn.DependsOn); err != nil {
			return nil, err
		}
		fn.Signature = signatureFromBytes(sig)
		if fn.CodeHash == "" {
			fn.CodeHash = HashFunctionCode(fn.SourceCode)
		}
		if fn.SignatureHash == "" {
			fn.SignatureHash = HashFunctionSignature(fn.Signature)
		}
		out = append(out, fn)
	}
	return out, nil
}

// preferBranchFunctions removes main-branch entries that have a
// branch-specific override at the same semver. Used by the branch-aware
// read paths to give the branch's published version priority while still
// inheriting unmodified versions from main (US-384).
func preferBranchFunctions(in []Function, branch string) []Function {
	if branch == "" || branch == DefaultBranch {
		return in
	}
	branchVersions := map[string]bool{}
	for _, fn := range in {
		if fn.BranchID == branch {
			branchVersions[fn.NormalisedVersion()] = true
		}
	}
	if len(branchVersions) == 0 {
		return in
	}
	out := in[:0]
	for _, fn := range in {
		if fn.BranchID != branch && branchVersions[fn.NormalisedVersion()] {
			continue
		}
		out = append(out, fn)
	}
	return out
}

func (r *PGRepository) ListFunctions(ctx context.Context, ontologyRID string) ([]Function, error) {
	branch := BranchScopeFromContext(ctx)
	// Order at the SQL layer is best-effort (lexical version sort would
	// place "10.0.0" before "2.0.0"); SortFunctionsByVersionDesc fixes that
	// in Go using parsed semver so callers see latest-first per name.
	rows, err := r.pool.Query(ctx,
		`SELECT rid, ontology_rid, name, version, source_code, COALESCE(created_by, ''),
		        COALESCE(signature, '{}'::jsonb), COALESCE(runtime, 'goja'), COALESCE(pure, FALSE), created_at,
		        COALESCE(code_hash, ''), COALESCE(signature_hash, ''), COALESCE(published_at, created_at),
		        COALESCE(branch_id, 'main'), COALESCE(depends_on, '{}'::text[])
		 FROM functions
		 WHERE (ontology_rid = $1 OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))
		 AND branch_id IN ($2, 'main')
		 ORDER BY name`, ontologyRID, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanFunctions(rows)
	if err != nil {
		return nil, err
	}
	out = preferBranchFunctionsByName(out, branch)
	SortFunctionsByVersionDesc(out)
	return out, nil
}

// preferBranchFunctionsByName removes main-branch entries for a given
// (name, version) pair when the branch overlay supplies its own version.
// Differs from preferBranchFunctions only in scope: this helper operates
// across multiple function names so the cross-name aggregate listing
// stays correct (US-384).
func preferBranchFunctionsByName(in []Function, branch string) []Function {
	if branch == "" || branch == DefaultBranch {
		return in
	}
	branchKeys := map[string]bool{}
	for _, fn := range in {
		if fn.BranchID == branch {
			branchKeys[fn.Name+"@"+fn.NormalisedVersion()] = true
		}
	}
	if len(branchKeys) == 0 {
		return in
	}
	out := in[:0]
	for _, fn := range in {
		if fn.BranchID != branch && branchKeys[fn.Name+"@"+fn.NormalisedVersion()] {
			continue
		}
		out = append(out, fn)
	}
	return out
}

func (r *PGRepository) UpdateFunction(ctx context.Context, fn *Function) error {
	signature := normaliseSignatureForWrite(fn.Signature)
	runtime := fn.NormalisedRuntime()
	version := fn.NormalisedVersion()
	codeHash := HashFunctionCode(fn.SourceCode)
	sigHash := HashFunctionSignature(signature)
	dependsOn := normaliseDependsOnForWrite(fn.DependsOn)
	tag, err := r.pool.Exec(ctx,
		`UPDATE functions SET name=$1, version=$2, source_code=$3, signature=$4, runtime=$5, pure=$6,
		   code_hash=$7, signature_hash=$8, published_at=NOW(), depends_on=$9
		 WHERE rid=$10`,
		fn.Name, version, fn.SourceCode, signature, runtime, fn.Pure, codeHash, sigHash, dependsOn, fn.RID)
	if err != nil {
		return wrapPGError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	fn.Runtime = runtime
	fn.Version = version
	fn.CodeHash = codeHash
	fn.SignatureHash = sigHash
	fn.DependsOn = dependsOn
	if len(signature) > 0 {
		fn.Signature = signature
	}
	return nil
}

func (r *PGRepository) DeleteFunction(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM functions WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- ActionLog ---

func (r *PGRepository) GetActionLog(ctx context.Context, id int64) (*ActionLog, error) {
	al := &ActionLog{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, action_type_rid, user_id, parameters, edits, COALESCE(prev_edits, 'null'),
		 status, COALESCE(error_message, ''), created_at,
		 COALESCE(side_effect_status, 'null')
		 FROM action_logs WHERE id = $1`, id).
		Scan(&al.ID, &al.ActionTypeRID, &al.UserID, &al.Parameters, &al.Edits,
			&al.PrevEdits, &al.Status, &al.ErrorMessage, &al.CreatedAt,
			&al.SideEffectStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Pre-migration rows (or actions with no side effects) come back as
	// JSON `null` thanks to COALESCE; normalize to nil so callers see the
	// natural Go zero value.
	if string(al.SideEffectStatus) == "null" {
		al.SideEffectStatus = nil
	}
	return al, nil
}

func (r *PGRepository) UpdateActionLogStatus(ctx context.Context, id int64, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE action_logs SET status = $1 WHERE id = $2`,
		status, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateActionLogSideEffectStatus stamps the per-effect dispatch outcome
// JSONB array onto the action_logs row. Passing a nil or zero-length
// status writes SQL NULL — callers can use this to clear the column or
// skip storage when the action had no side effects. PRD-V2 Gap-A4.
func (r *PGRepository) UpdateActionLogSideEffectStatus(ctx context.Context, id int64, status json.RawMessage) error {
	var arg interface{}
	if len(status) == 0 {
		arg = nil
	} else {
		arg = []byte(status)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE action_logs SET side_effect_status = $1 WHERE id = $2`,
		arg, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) ListActionLogs(ctx context.Context, actionTypeRID string, limit, offset int) ([]ActionLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, action_type_rid, user_id, parameters, edits, status,
		 COALESCE(error_message, ''), created_at,
		 COALESCE(side_effect_status, 'null')
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
			&al.Edits, &al.Status, &al.ErrorMessage, &al.CreatedAt,
			&al.SideEffectStatus); err != nil {
			return nil, err
		}
		if string(al.SideEffectStatus) == "null" {
			al.SideEffectStatus = nil
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

func (r *PGRepository) InsertActionLog(ctx context.Context, al *ActionLog) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO action_logs (action_type_rid, user_id, parameters, edits, status, error_message, prev_edits)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		al.ActionTypeRID, al.UserID, al.Parameters, al.Edits, al.Status, nilIfEmpty(al.ErrorMessage), al.PrevEdits).
		Scan(&al.ID, &al.CreatedAt)
	if err != nil {
		return err
	}
	return nil
}

// WriteActionLogsAtomic persists the given action log rows in a single
// PostgreSQL transaction. All rows commit together or nothing is written —
// this is the PG side of the US-238 atomic-batch guarantee. Each row's ID
// and CreatedAt are back-filled on success; on failure the caller receives a
// raw pgx error and no rows are visible after rollback.
func (r *PGRepository) WriteActionLogsAtomic(ctx context.Context, logs []*ActionLog) error {
	if len(logs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, al := range logs {
		if err := tx.QueryRow(ctx,
			`INSERT INTO action_logs (action_type_rid, user_id, parameters, edits, status, error_message, prev_edits)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id, created_at`,
			al.ActionTypeRID, al.UserID, al.Parameters, al.Edits, al.Status, nilIfEmpty(al.ErrorMessage), al.PrevEdits).
			Scan(&al.ID, &al.CreatedAt); err != nil {
			return fmt.Errorf("insert action log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// --- ObjectHistory (Tier 2.3) ---

// InsertObjectHistory writes a new history row and back-fills the generated
// id and recorded_at timestamps onto h. nil PrevState/NewState are stored as
// SQL NULL so DELETE rows do not carry stale state.
//
// When h.RecordedAt is non-zero the caller-provided value is written to the
// column so US-021 conflict resolution can compare ingest batch timestamps
// against the user edit time that the producer stamped. A zero value falls
// through to the DEFAULT NOW() behaviour preserved from Tier 2.3.
func (r *PGRepository) InsertObjectHistory(ctx context.Context, h *ObjectHistory) error {
	source := h.Source
	if source == "" {
		source = EditSourceUser
	}
	var err error
	if !h.RecordedAt.IsZero() {
		err = r.pool.QueryRow(ctx,
			`INSERT INTO object_history
			   (object_type_rid, ontology_rid, primary_key, version, prev_state, new_state,
			    edit_type, source, action_log_rid, user_id, recorded_at, valid_from, tx_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, $12)
			 RETURNING id, recorded_at`,
			h.ObjectTypeRID, nilIfEmpty(h.OntologyRID), h.PrimaryKey, h.Version,
			nilIfNoBytes(h.PrevState), nilIfNoBytes(h.NewState),
			h.EditType, source, nilIfEmpty(h.ActionLogRID), nilIfEmpty(h.UserID),
			h.RecordedAt, nilIfEmpty(h.TxID)).
			Scan(&h.ID, &h.RecordedAt)
	} else {
		err = r.pool.QueryRow(ctx,
			`INSERT INTO object_history
			   (object_type_rid, ontology_rid, primary_key, version, prev_state, new_state,
			    edit_type, source, action_log_rid, user_id, valid_from, tx_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), $11)
			 RETURNING id, recorded_at`,
			h.ObjectTypeRID, nilIfEmpty(h.OntologyRID), h.PrimaryKey, h.Version,
			nilIfNoBytes(h.PrevState), nilIfNoBytes(h.NewState),
			h.EditType, source, nilIfEmpty(h.ActionLogRID), nilIfEmpty(h.UserID),
			nilIfEmpty(h.TxID)).
			Scan(&h.ID, &h.RecordedAt)
	}
	if err != nil {
		return err
	}
	// US-223: close out the prior open version of (object_type_rid, primary_key)
	// so a snapshot read at time T resolves to exactly one row per PK. Any
	// row with the same (object_type_rid, primary_key) and version < ours
	// whose valid_to is still NULL is the previous "live" version; stamp its
	// valid_to with our own valid_from (== recorded_at) so the [valid_from,
	// valid_to) range becomes contiguous and non-overlapping.
	if _, err := r.pool.Exec(ctx,
		`UPDATE object_history
		    SET valid_to = $3
		  WHERE object_type_rid = $1
		    AND primary_key = $2
		    AND version < $4
		    AND valid_to IS NULL`,
		h.ObjectTypeRID, h.PrimaryKey, h.RecordedAt, h.Version); err != nil {
		return err
	}
	h.Source = source
	return nil
}

// LatestUserEditAt returns the recorded_at of the most recent history row
// whose source == 'user' for (objectTypeRID, primaryKey). Used by the funnel
// consumer's US-021 user-edit-wins conflict resolver. The second return
// value is false when no user edit exists, in which case the caller should
// treat ingest edits as the authoritative source.
func (r *PGRepository) LatestUserEditAt(ctx context.Context, objectTypeRID, primaryKey string) (time.Time, bool, error) {
	var ts time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT recorded_at FROM object_history
		 WHERE object_type_rid = $1 AND primary_key = $2
		   AND COALESCE(source, 'user') = 'user'
		 ORDER BY recorded_at DESC
		 LIMIT 1`,
		objectTypeRID, primaryKey).Scan(&ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return ts, true, nil
}

// ListObjectHistoryPage returns up to `limit` history rows for a given
// (objectTypeRID, primaryKey) tuple, ordered by version DESC. When
// beforeVersion > 0 the result is constrained to rows with
// `version < beforeVersion`, enabling cursor-based pagination that walks the
// timeline backwards in time. Pass beforeVersion=0 to fetch the newest page.
func (r *PGRepository) ListObjectHistoryPage(ctx context.Context, objectTypeRID, primaryKey string, beforeVersion int64, limit int) ([]ObjectHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if beforeVersion > 0 {
		rows, err = r.pool.Query(ctx,
			`SELECT id, COALESCE(ontology_rid, ''), object_type_rid, primary_key, version,
			        prev_state, new_state, edit_type,
			        COALESCE(source, 'user'),
			        COALESCE(action_log_rid, ''), COALESCE(user_id, ''),
			        recorded_at
			 FROM object_history
			 WHERE object_type_rid = $1 AND primary_key = $2 AND version < $3
			 ORDER BY version DESC
			 LIMIT $4`,
			objectTypeRID, primaryKey, beforeVersion, limit)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT id, COALESCE(ontology_rid, ''), object_type_rid, primary_key, version,
			        prev_state, new_state, edit_type,
			        COALESCE(source, 'user'),
			        COALESCE(action_log_rid, ''), COALESCE(user_id, ''),
			        recorded_at
			 FROM object_history
			 WHERE object_type_rid = $1 AND primary_key = $2
			 ORDER BY version DESC
			 LIMIT $3`,
			objectTypeRID, primaryKey, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ObjectHistory
	for rows.Next() {
		var h ObjectHistory
		var prev, next []byte
		if err := rows.Scan(&h.ID, &h.OntologyRID, &h.ObjectTypeRID, &h.PrimaryKey, &h.Version,
			&prev, &next, &h.EditType, &h.Source,
			&h.ActionLogRID, &h.UserID, &h.RecordedAt); err != nil {
			return nil, err
		}
		if len(prev) > 0 {
			h.PrevState = append(h.PrevState[:0], prev...)
		}
		if len(next) > 0 {
			h.NewState = append(h.NewState[:0], next...)
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// ListObjectHistory returns the most recent `limit` history rows for a given
// (objectTypeRID, primaryKey) tuple, ordered by version DESC.
func (r *PGRepository) ListObjectHistory(ctx context.Context, objectTypeRID, primaryKey string, limit int) ([]ObjectHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, COALESCE(ontology_rid, ''), object_type_rid, primary_key, version,
		        prev_state, new_state, edit_type,
		        COALESCE(source, 'user'),
		        COALESCE(action_log_rid, ''), COALESCE(user_id, ''),
		        recorded_at
		 FROM object_history
		 WHERE object_type_rid = $1 AND primary_key = $2
		 ORDER BY version DESC
		 LIMIT $3`,
		objectTypeRID, primaryKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ObjectHistory
	for rows.Next() {
		var h ObjectHistory
		var prev, next []byte
		if err := rows.Scan(&h.ID, &h.OntologyRID, &h.ObjectTypeRID, &h.PrimaryKey, &h.Version,
			&prev, &next, &h.EditType, &h.Source,
			&h.ActionLogRID, &h.UserID, &h.RecordedAt); err != nil {
			return nil, err
		}
		if len(prev) > 0 {
			h.PrevState = append(h.PrevState[:0], prev...)
		}
		if len(next) > 0 {
			h.NewState = append(h.NewState[:0], next...)
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// GetObjectVersionCount returns the total number of history rows recorded
// for a given (objectTypeRID, primaryKey) tuple. Returns 0 (not an error)
// when no history exists.
func (r *PGRepository) GetObjectVersionCount(ctx context.Context, objectTypeRID, primaryKey string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM object_history
		 WHERE object_type_rid = $1 AND primary_key = $2`,
		objectTypeRID, primaryKey).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// LoadLatestObjectStates returns the newest non-tombstoned new_state for
// every primary key that has any history row for the given ObjectType RID.
// Rows whose latest version is a DELETE are skipped: a rebuilt index must
// not resurrect deleted objects.
//
// The SELECT DISTINCT ON pattern matches one row per primary_key sorted by
// version DESC, so concurrent writers that produced multiple versions for
// the same key leave the index consistent with the authoritative tail.
func (r *PGRepository) LoadLatestObjectStates(ctx context.Context, objectTypeRID string) ([]LatestObjectState, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (primary_key) primary_key, new_state, edit_type
		 FROM object_history
		 WHERE object_type_rid = $1
		 ORDER BY primary_key, version DESC`,
		objectTypeRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []LatestObjectState
	for rows.Next() {
		var pk, editType string
		var newState []byte
		if err := rows.Scan(&pk, &newState, &editType); err != nil {
			return nil, err
		}
		if editType == "DELETE" || len(newState) == 0 {
			continue
		}
		buf := make([]byte, len(newState))
		copy(buf, newState)
		result = append(result, LatestObjectState{
			PrimaryKey: pk,
			NewState:   buf,
		})
	}
	return result, rows.Err()
}

// SnapshotObjectsAt returns the per-PK new_state of every primary_key whose
// validity window covers asOf for the given ObjectType RID. The validity
// window is `[valid_from, valid_to)` — half-open so adjacent versions never
// double-count. Rows whose covering version is a DELETE tombstone are
// skipped: the caller should treat the absence of an entry as "object did
// not exist at asOf". US-223.
func (r *PGRepository) SnapshotObjectsAt(ctx context.Context, objectTypeRID string, asOf time.Time) ([]LatestObjectState, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT primary_key, new_state, edit_type
		   FROM object_history
		  WHERE object_type_rid = $1
		    AND valid_from <= $2
		    AND (valid_to IS NULL OR valid_to > $2)`,
		objectTypeRID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []LatestObjectState
	for rows.Next() {
		var pk, editType string
		var newState []byte
		if err := rows.Scan(&pk, &newState, &editType); err != nil {
			return nil, err
		}
		if editType == "DELETE" || len(newState) == 0 {
			continue
		}
		buf := make([]byte, len(newState))
		copy(buf, newState)
		result = append(result, LatestObjectState{
			PrimaryKey: pk,
			NewState:   buf,
		})
	}
	return result, rows.Err()
}

// nilIfNoBytes returns nil for an empty/nil byte slice so jsonb columns
// receive SQL NULL rather than the literal "" / "null" payload.
func nilIfNoBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
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

// --- ObjectEmbedding (Tier 3.1) ---
//
// These methods speak to the `object_embeddings` table created by
// migration 000011. They use github.com/pgvector/pgvector-go's Vector type
// in TEXT format on the wire so we don't need to register a custom pgx
// codec — pgvector accepts the `[v1,v2,v3]` literal on INSERT and emits
// the same form on SELECT.

// UpsertObjectEmbedding inserts a new embedding row or, if one already
// exists for the (objectTypeRID, primaryKey, model) tuple, replaces the
// vector and source_text in place. CreatedAt / UpdatedAt on the input
// struct are populated from the row that ends up in the database so the
// caller can log or test against them.
func (r *PGRepository) UpsertObjectEmbedding(ctx context.Context, e *ObjectEmbedding) error {
	if e == nil {
		return fmt.Errorf("UpsertObjectEmbedding: nil embedding")
	}
	if len(e.Embedding) == 0 {
		return fmt.Errorf("UpsertObjectEmbedding: empty embedding vector")
	}
	vec := pgvector.NewVector(e.Embedding)
	err := r.pool.QueryRow(ctx,
		`INSERT INTO object_embeddings
		   (object_type_rid, primary_key, embedding, model, source_text)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (object_type_rid, primary_key, model)
		 DO UPDATE SET embedding = EXCLUDED.embedding,
		               source_text = EXCLUDED.source_text,
		               updated_at = NOW()
		 RETURNING created_at, updated_at`,
		e.ObjectTypeRID, e.PrimaryKey, vec, e.Model, nilIfEmpty(e.SourceText)).
		Scan(&e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert object embedding: %w", err)
	}
	return nil
}

// GetObjectEmbedding returns the stored embedding for a single
// (objectTypeRID, primaryKey, model) tuple, or ErrNotFound when no row
// exists. The returned struct shares ownership of the float32 slice with
// the parsed pgvector.Vector — callers MUST treat it as read-only.
func (r *PGRepository) GetObjectEmbedding(ctx context.Context, objectTypeRID, primaryKey, model string) (*ObjectEmbedding, error) {
	var (
		vec        pgvector.Vector
		sourceText *string
		out        ObjectEmbedding
	)
	err := r.pool.QueryRow(ctx,
		`SELECT object_type_rid, primary_key, embedding, model, source_text, created_at, updated_at
		 FROM object_embeddings
		 WHERE object_type_rid = $1 AND primary_key = $2 AND model = $3`,
		objectTypeRID, primaryKey, model).
		Scan(&out.ObjectTypeRID, &out.PrimaryKey, &vec, &out.Model, &sourceText, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get object embedding: %w", err)
	}
	out.Embedding = vec.Slice()
	if sourceText != nil {
		out.SourceText = *sourceText
	}
	return &out, nil
}

// FindNearestNeighbors runs a kNN cosine-distance query against the HNSW
// index and returns up to k closest rows for the given object type and
// model. Results are ordered by ascending distance (closest first).
//
// k is clamped to a sensible maximum to protect against runaway requests.
// The model filter is applied as a WHERE clause so the planner can use
// the (object_type_rid, primary_key) btree before walking the HNSW graph
// — the order matters because HNSW is approximate, so reducing the
// candidate set first generally yields more accurate top-k results.
func (r *PGRepository) FindNearestNeighbors(ctx context.Context, objectTypeRID string, queryVector []float32, k int, model string) ([]NearestNeighborResult, error) {
	if k <= 0 {
		k = 10
	}
	if k > 1000 {
		k = 1000
	}
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("FindNearestNeighbors: empty query vector")
	}
	vec := pgvector.NewVector(queryVector)
	rows, err := r.pool.Query(ctx,
		`SELECT primary_key, embedding <=> $1 AS distance
		 FROM object_embeddings
		 WHERE object_type_rid = $2 AND model = $3
		 ORDER BY embedding <=> $1
		 LIMIT $4`,
		vec, objectTypeRID, model, k)
	if err != nil {
		return nil, fmt.Errorf("nearest neighbors query: %w", err)
	}
	defer rows.Close()

	var results []NearestNeighborResult
	for rows.Next() {
		var (
			pk       string
			distance float64
		)
		if err := rows.Scan(&pk, &distance); err != nil {
			return nil, fmt.Errorf("scan nearest neighbor row: %w", err)
		}
		results = append(results, NearestNeighborResult{
			PrimaryKey: pk,
			Distance:   float32(distance),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nearest neighbors: %w", err)
	}
	return results, nil
}

// --- OntologyBranch (Phase 2) ---

func (r *PGRepository) CreateBranch(ctx context.Context, b *OntologyBranch) error {
	b.Status = NormalizeBranchStatus(b.Status)
	err := r.pool.QueryRow(ctx,
		`INSERT INTO ontology_branches (id, ontology_rid, name, base_version, parent_branch_id, base_tx, status, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING created_at, updated_at`,
		b.ID, b.OntologyRID, b.Name, b.BaseVersion,
		nilIfEmpty(b.ParentBranchID), nilIfEmpty(b.BaseTx),
		b.Status, b.CreatedBy).
		Scan(&b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetBranch(ctx context.Context, id string) (*OntologyBranch, error) {
	b := &OntologyBranch{}
	var parentBranchID, baseTx *string
	err := r.pool.QueryRow(ctx,
		`SELECT id, ontology_rid, name, base_version, parent_branch_id, base_tx, status, created_by, created_at, updated_at
		 FROM ontology_branches WHERE id = $1`, id).
		Scan(&b.ID, &b.OntologyRID, &b.Name, &b.BaseVersion, &parentBranchID, &baseTx,
			&b.Status, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parentBranchID != nil {
		b.ParentBranchID = *parentBranchID
	}
	if baseTx != nil {
		b.BaseTx = *baseTx
	}
	return b, nil
}

func (r *PGRepository) ListBranches(ctx context.Context, ontologyRID string) ([]OntologyBranch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, ontology_rid, name, base_version, parent_branch_id, base_tx, status, created_by, created_at, updated_at
		 FROM ontology_branches WHERE ontology_rid = $1 ORDER BY created_at`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []OntologyBranch
	for rows.Next() {
		var b OntologyBranch
		var parentBranchID, baseTx *string
		if err := rows.Scan(&b.ID, &b.OntologyRID, &b.Name, &b.BaseVersion, &parentBranchID, &baseTx,
			&b.Status, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		if parentBranchID != nil {
			b.ParentBranchID = *parentBranchID
		}
		if baseTx != nil {
			b.BaseTx = *baseTx
		}
		result = append(result, b)
	}
	return result, nil
}

func (r *PGRepository) CloseBranch(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontology_branches SET status = 'closed', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) UpdateBranchStatus(ctx context.Context, id, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontology_branches SET status = $2, updated_at = NOW() WHERE id = $1`, id, NormalizeBranchStatus(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) UpdateBranchBaseVersion(ctx context.Context, id string, baseVersion int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontology_branches SET base_version = $2, updated_at = NOW() WHERE id = $1`, id, baseVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) CreateBranchChange(ctx context.Context, c *BranchChange) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO ontology_branch_changes (id, branch_id, change_type, entity_type, entity_rid, before_state, after_state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		c.ID, c.BranchID, c.ChangeType, c.EntityType, c.EntityRID, nilIfNoBytes(c.BeforeState), nilIfNoBytes(c.AfterState)).
		Scan(&c.CreatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListBranchChanges(ctx context.Context, branchID string) ([]BranchChange, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, branch_id, change_type, entity_type, entity_rid, before_state, after_state, created_at
		 FROM ontology_branch_changes WHERE branch_id = $1 ORDER BY created_at`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BranchChange
	for rows.Next() {
		var c BranchChange
		if err := rows.Scan(&c.ID, &c.BranchID, &c.ChangeType, &c.EntityType, &c.EntityRID, &c.BeforeState, &c.AfterState, &c.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *PGRepository) UpdateBranchChangeBeforeState(ctx context.Context, id string, beforeState json.RawMessage) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontology_branch_changes SET before_state = $2 WHERE id = $1`, id, nilIfNoBytes(beforeState))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- OntologyProposal (US-117) ---

func (r *PGRepository) CreateProposal(ctx context.Context, p *OntologyProposal) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO ontology_proposals (id, branch_id, ontology_rid, title, description, status, author)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at, updated_at`,
		p.ID, p.BranchID, p.OntologyRID, p.Title, p.Description, p.Status, p.Author).
		Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetProposal(ctx context.Context, id string) (*OntologyProposal, error) {
	p := &OntologyProposal{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, branch_id, ontology_rid, title, description, status, author, created_at, updated_at
		 FROM ontology_proposals WHERE id = $1`, id).
		Scan(&p.ID, &p.BranchID, &p.OntologyRID, &p.Title, &p.Description, &p.Status, &p.Author, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *PGRepository) ListProposals(ctx context.Context, ontologyRID string) ([]OntologyProposal, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, branch_id, ontology_rid, title, description, status, author, created_at, updated_at
		 FROM ontology_proposals WHERE ontology_rid = $1 ORDER BY created_at`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []OntologyProposal
	for rows.Next() {
		var p OntologyProposal
		if err := rows.Scan(&p.ID, &p.BranchID, &p.OntologyRID, &p.Title, &p.Description, &p.Status, &p.Author, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *PGRepository) UpdateProposalStatus(ctx context.Context, id, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ontology_proposals SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) CreateProposalReview(ctx context.Context, rv *ProposalReview) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO proposal_reviews (id, proposal_id, reviewer, decision, reason)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		rv.ID, rv.ProposalID, rv.Reviewer, rv.Decision, rv.Reason).
		Scan(&rv.CreatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListProposalReviews(ctx context.Context, proposalID string) ([]ProposalReview, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, proposal_id, reviewer, decision, reason, created_at
		 FROM proposal_reviews WHERE proposal_id = $1 ORDER BY created_at`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProposalReview
	for rows.Next() {
		var rv ProposalReview
		if err := rows.Scan(&rv.ID, &rv.ProposalID, &rv.Reviewer, &rv.Decision, &rv.Reason, &rv.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, rv)
	}
	return result, nil
}

// --- AutomationRule ---

func (r *PGRepository) CreateAutomationRule(ctx context.Context, rule *AutomationRule) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO automation_rules (id, ontology_rid, name, description, status, trigger_type, trigger_config, effects, retry_policy, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING created_at, updated_at`,
		rule.ID, rule.OntologyRID, rule.Name, rule.Description, rule.Status, rule.TriggerType, rule.TriggerConfig, rule.Effects, rule.RetryPolicy, rule.CreatedBy).
		Scan(&rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) GetAutomationRule(ctx context.Context, id string) (*AutomationRule, error) {
	rule := &AutomationRule{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, ontology_rid, name, description, status, trigger_type, trigger_config, effects, retry_policy, created_by, created_at, updated_at
		 FROM automation_rules WHERE id = $1`, id).
		Scan(&rule.ID, &rule.OntologyRID, &rule.Name, &rule.Description, &rule.Status, &rule.TriggerType, &rule.TriggerConfig, &rule.Effects, &rule.RetryPolicy, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rule, nil
}

func (r *PGRepository) ListAutomationRules(ctx context.Context, ontologyRID string) ([]AutomationRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, ontology_rid, name, description, status, trigger_type, trigger_config, effects, retry_policy, created_by, created_at, updated_at
		 FROM automation_rules WHERE ontology_rid = $1 ORDER BY created_at`, ontologyRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AutomationRule
	for rows.Next() {
		var rule AutomationRule
		if err := rows.Scan(&rule.ID, &rule.OntologyRID, &rule.Name, &rule.Description, &rule.Status, &rule.TriggerType, &rule.TriggerConfig, &rule.Effects, &rule.RetryPolicy, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, nil
}

func (r *PGRepository) UpdateAutomationRule(ctx context.Context, rule *AutomationRule) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE automation_rules SET name = $2, description = $3, status = $4, trigger_type = $5, trigger_config = $6, effects = $7, retry_policy = $8, updated_at = NOW()
		 WHERE id = $1`,
		rule.ID, rule.Name, rule.Description, rule.Status, rule.TriggerType, rule.TriggerConfig, rule.Effects, rule.RetryPolicy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) DeleteAutomationRule(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM automation_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- AutomationExecution ---

func (r *PGRepository) InsertExecution(ctx context.Context, exec *AutomationExecution) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO automation_executions (id, rule_id, trigger_event, started_at, completed_at, status, error, retry_count, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		exec.ID, exec.RuleID, exec.TriggerEvent, exec.StartedAt, exec.CompletedAt, exec.Status, exec.Error, exec.RetryCount, exec.Result)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) UpdateExecution(ctx context.Context, exec *AutomationExecution) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE automation_executions SET completed_at = $2, status = $3, error = $4, retry_count = $5, result = $6
		 WHERE id = $1`,
		exec.ID, exec.CompletedAt, exec.Status, exec.Error, exec.RetryCount, exec.Result)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGRepository) ListExecutions(ctx context.Context, ruleID string) ([]AutomationExecution, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, rule_id, trigger_event, started_at, completed_at, status, error, retry_count, result
		 FROM automation_executions WHERE rule_id = $1 ORDER BY started_at`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AutomationExecution
	for rows.Next() {
		var exec AutomationExecution
		if err := rows.Scan(&exec.ID, &exec.RuleID, &exec.TriggerEvent, &exec.StartedAt, &exec.CompletedAt, &exec.Status, &exec.Error, &exec.RetryCount, &exec.Result); err != nil {
			return nil, err
		}
		result = append(result, exec)
	}
	return result, nil
}

// --- Notification (US-130) ---

func (r *PGRepository) CreateNotification(ctx context.Context, n *Notification) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notifications (id, user_id, title, body, type, link, read, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		n.ID, n.UserID, n.Title, n.Body, n.Type, n.Link, n.Read, n.CreatedAt)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

func (r *PGRepository) ListNotifications(ctx context.Context, userID string, unreadOnly bool) ([]Notification, error) {
	query := `SELECT id, user_id, title, body, type, link, read, created_at
	          FROM notifications WHERE user_id = $1`
	if unreadOnly {
		query += ` AND read = false`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Body, &n.Type, &n.Link, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, nil
}

func (r *PGRepository) MarkNotificationRead(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read = true WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllNotificationsRead marks every unread notification belonging to userID
// as read. When types is non-empty the update is scoped to rows whose type is
// in that set. Returns the number of rows updated. Implements the narrow
// NotificationBulkStore interface (US-343).
func (r *PGRepository) MarkAllNotificationsRead(ctx context.Context, userID string, types []string) (int, error) {
	query := `UPDATE notifications SET read = true WHERE user_id = $1 AND read = false`
	args := []interface{}{userID}
	if len(types) > 0 {
		query += ` AND type = ANY($2)`
		args = append(args, types)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
