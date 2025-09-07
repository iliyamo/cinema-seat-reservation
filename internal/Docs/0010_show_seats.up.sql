-- Migration to add show_seats table which represents the availability of
-- specific seats for a given show.  Each record links a seat in a hall to
-- a show scheduled in that hall, with its own price and status.

CREATE TABLE IF NOT EXISTS show_seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  status ENUM('FREE','HELD','RESERVED') NOT NULL DEFAULT 'FREE',
  price_cents INT UNSIGNED NOT NULL DEFAULT 0,
  version INT UNSIGNED NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_show_seat (show_id, seat_id),
  KEY idx_show (show_id),
  KEY idx_seat (seat_id),
  CONSTRAINT fk_show_seats_show FOREIGN KEY (show_id) REFERENCES shows(id) ON DELETE CASCADE,
  CONSTRAINT fk_show_seats_seat FOREIGN KEY (seat_id) REFERENCES seats(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;