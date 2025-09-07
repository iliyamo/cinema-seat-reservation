-- Rollback for 0008_cinemas.up.sql
-- Drop the foreign key and columns added to the halls table, then drop
-- the cinemas table.  This migration reverses the schema changes
-- introduced in the corresponding up migration.

-- Remove foreign key and columns from halls
ALTER TABLE halls
  DROP FOREIGN KEY fk_halls_cinema,
  DROP COLUMN cinema_id,
  DROP COLUMN seat_rows,
  DROP COLUMN seat_cols;

-- Drop cinemas table
DROP TABLE IF EXISTS cinemas;