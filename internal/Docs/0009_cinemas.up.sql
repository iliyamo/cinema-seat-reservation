-- Migration to add cinemas and extend halls with cinema association and seating dimensions
-- This migration introduces a new 'cinemas' table to group halls under an
-- owner and adds columns to the existing 'halls' table to associate each
-- hall with a cinema and to specify the layout of the hall (seat_rows × seat_cols).

-- Create cinemas table: each cinema belongs to one owner and has a unique
-- name per owner.  A cinema represents a physical movie venue and can
-- contain multiple halls.  Ownership is enforced at the application layer.
CREATE TABLE IF NOT EXISTS cinemas (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_owner_name (owner_id, name),
  CONSTRAINT fk_cinemas_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Extend halls table: associate each hall with a cinema and record the
-- number of seat seat_rows and columns.  These columns are nullable to
-- support backward compatibility but should be provided for new halls.
ALTER TABLE halls
  ADD COLUMN cinema_id BIGINT UNSIGNED NULL AFTER owner_id,
  ADD COLUMN seat_rows INT UNSIGNED NULL AFTER description,
  ADD COLUMN seat_cols INT UNSIGNED NULL AFTER seat_rows,
  ADD CONSTRAINT fk_halls_cinema FOREIGN KEY (cinema_id) REFERENCES cinemas(id) ON DELETE RESTRICT,
  -- Drop the old unique index on (owner_id, name) if it exists and replace
  -- it with a new composite non-unique index including cinema_id.  This allows
  -- owners to reuse hall names across different cinemas.  Enforcement of duplicate
  -- halls with identical attributes is handled in application code.
  DROP INDEX uk_owner_name,
  ADD KEY idx_owner_cinema_name (owner_id, cinema_id, name);