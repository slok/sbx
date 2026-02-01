-- Add Firecracker-specific fields to sandboxes table
ALTER TABLE sandboxes ADD COLUMN pid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sandboxes ADD COLUMN socket_path TEXT NOT NULL DEFAULT '';
ALTER TABLE sandboxes ADD COLUMN tap_device TEXT NOT NULL DEFAULT '';
ALTER TABLE sandboxes ADD COLUMN internal_ip TEXT NOT NULL DEFAULT '';
