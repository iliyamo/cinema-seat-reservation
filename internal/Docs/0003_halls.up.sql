-- Halls: each hall owned by a user with role OWNER (enforced at app layer)
-- Create the halls table.  Each hall belongs to an owner and may
-- optionally be associated with a cinema.  The unique constraint
-- includes the cinema_id so that owners can reuse hall names across
-- different cinemas.  Without cinema_id in the index MySQL would
-- reject creating two halls with the same name even if they belong
-- to separate cinemas, which is undesirable.  Note: the cinema_id
-- column is added in a later migration (0009_cinemas.up.sql).
CREATE TABLE IF NOT EXISTS halls (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  description TEXT NULL,
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  -- enforce unique hall names per owner.  This index will later be
  -- updated to include cinema_id once that column is added in a
  -- subsequent migration.  The default here preserves backwards
  -- compatibility for installations that have not yet adopted the
  -- cinema feature.
  UNIQUE KEY uk_owner_name (owner_id, name),
  KEY idx_owner (owner_id),
  CONSTRAINT fk_halls_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
