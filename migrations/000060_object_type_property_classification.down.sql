-- Reverse of 000060_object_type_property_classification.up.sql.

ALTER TABLE object_types DROP COLUMN IF EXISTS classification;
ALTER TABLE properties DROP COLUMN IF EXISTS classification;
