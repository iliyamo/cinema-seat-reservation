-- Shows: a movie showtime scheduled in a hall
CREATE TABLE IF NOT EXISTS shows (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  hall_id BIGINT UNSIGNED NOT NULL,
  title VARCHAR(255) NOT NULL,                   -- movie title or external ref
  starts_at DATETIME NOT NULL,
  ends_at DATETIME NOT NULL,
  base_price_cents INT UNSIGNED NOT NULL DEFAULT 0,
  status ENUM('SCHEDULED','CANCELLED','FINISHED') NOT NULL DEFAULT 'SCHEDULED',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_hall_time (hall_id, starts_at),
  CONSTRAINT chk_show_time CHECK (ends_at > starts_at),
  CONSTRAINT fk_shows_hall FOREIGN KEY (hall_id) REFERENCES halls(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- NOTE: avoiding a DB-level "no-overlap" constraint; enforce overlap checks in app/service layer.
