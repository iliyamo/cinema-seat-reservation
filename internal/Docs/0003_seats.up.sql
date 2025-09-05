-- Seats: physical seats in a hall
CREATE TABLE IF NOT EXISTS seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  hall_id BIGINT UNSIGNED NOT NULL,
  row_label VARCHAR(8) NOT NULL,                 -- e.g. A, B, C or 1,2,3
  seat_number INT UNSIGNED NOT NULL,             -- e.g. 1..30
  seat_type ENUM('STANDARD','VIP','ACCESSIBLE') NOT NULL DEFAULT 'STANDARD',
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_hall_row_no (hall_id, row_label, seat_number),
  KEY idx_seats_hall (hall_id),
  CONSTRAINT fk_seats_hall FOREIGN KEY (hall_id) REFERENCES halls(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
