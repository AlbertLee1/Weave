-- Add outgoing_link_types JSONB column to interfaces table.
-- Stores InterfaceLinkType definitions analogous to shared_properties.
ALTER TABLE interfaces ADD COLUMN IF NOT EXISTS outgoing_link_types JSONB DEFAULT '[]';
