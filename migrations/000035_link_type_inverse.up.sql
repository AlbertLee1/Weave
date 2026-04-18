-- US-209: Bidirectional Links. Adds a self-referential pointer on link_types
-- so an ObjectType modeler can declare that link A is the inverse of link B
-- (e.g. emp -> dept + dept -> emp). The column is nullable because (1) the
-- default remains uni-directional, and (2) the pair is typically created one
-- side at a time, so cross-references must be allowed to temporarily dangle.
-- Handler-level validation enforces that the partner's (source, target)
-- object types are a mirror of this row's (target, source).

ALTER TABLE link_types
    ADD COLUMN IF NOT EXISTS inverse_link_rid TEXT;

CREATE INDEX IF NOT EXISTS link_types_inverse_rid_idx
    ON link_types(inverse_link_rid)
    WHERE inverse_link_rid IS NOT NULL;
