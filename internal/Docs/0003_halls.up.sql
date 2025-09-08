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
  -- index hall names per owner.  This index is non-unique because duplicate names are allowed;
  -- enforcement of identical hall attributes is handled in the application layer.
  KEY idx_owner_name (owner_id, name),
  KEY idx_owner (owner_id),
  CONSTRAINT fk_halls_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
