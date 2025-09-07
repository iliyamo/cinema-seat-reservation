-- Reservation seats: line-items mapping reservation to concrete seats for a show
CREATE TABLE IF NOT EXISTS reservation_seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  reservation_id BIGINT UNSIGNED NOT NULL,
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  price_cents INT UNSIGNED NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),

  -- Prevent double-selling the same seat for the same show (across all reservations)
  UNIQUE KEY uk_reserved_once (show_id, seat_id),

  KEY idx_reservation (reservation_id),
  KEY idx_show (show_id),
  KEY idx_seat (seat_id),

  CONSTRAINT fk_resseat_reservation FOREIGN KEY (reservation_id) REFERENCES reservations(id) ON DELETE CASCADE,
  CONSTRAINT fk_resseat_show        FOREIGN KEY (show_id)        REFERENCES shows(id)        ON DELETE RESTRICT,
  CONSTRAINT fk_resseat_seat        FOREIGN KEY (seat_id)        REFERENCES seats(id)        ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
