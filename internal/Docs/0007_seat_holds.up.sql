-- Seat holds: temporary locks during checkout (deleted on confirm/expire/release)
CREATE TABLE IF NOT EXISTS seat_holds (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,                   -- optional (guest holds)
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  hold_token CHAR(64) NOT NULL,                   -- random token for client correlation
  expires_at DATETIME NOT NULL,                   -- enforce via app/cron: purge expired
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),

  UNIQUE KEY uk_hold_token (hold_token),          -- quick lookup by token
  UNIQUE KEY uk_active_hold (show_id, seat_id),   -- at most one active hold per seat+show
  KEY idx_hold_expires (expires_at),
  KEY idx_hold_show (show_id),

  CONSTRAINT chk_hold_future CHECK (expires_at > created_at),
  CONSTRAINT fk_hold_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_hold_show FOREIGN KEY (show_id) REFERENCES shows(id) ON DELETE RESTRICT,
  CONSTRAINT fk_hold_seat FOREIGN KEY (seat_id) REFERENCES seats(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Tip: schedule a job to run:
--   DELETE FROM seat_holds WHERE expires_at < UTC_TIMESTAMP();
