-- Reverse US-025 properties.is_edit_only column.

ALTER TABLE properties DROP COLUMN IF EXISTS is_edit_only;
