-- cinema_updated.sql
-- Clean bootstrap schema aligned with /Docs migrations (through 0010 + 0011 indexes)
-- Compatible with MySQL 5.7+/8.0+ and MariaDB 10.4+
-- Engine: InnoDB, Charset: utf8mb4, Collation: utf8mb4_unicode_ci

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ROLES
CREATE TABLE IF NOT EXISTS roles (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  name VARCHAR(64) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_roles_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- USERS
CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  email VARCHAR(320) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role_id INT UNSIGNED NULL,
  display_name VARCHAR(128) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_email (email),
  KEY idx_users_role_id (role_id),
  CONSTRAINT fk_users_role FOREIGN KEY (role_id) REFERENCES roles(id)
    ON UPDATE CASCADE ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- CINEMAS
CREATE TABLE IF NOT EXISTS cinemas (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_cinemas_owner_name (owner_id, name),
  KEY idx_cinemas_owner_id (owner_id),
  CONSTRAINT fk_cinemas_owner FOREIGN KEY (owner_id) REFERENCES users(id)
    ON UPDATE CASCADE ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- HALLS
CREATE TABLE IF NOT EXISTS halls (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id BIGINT UNSIGNED NULL, -- legacy (kept for compatibility; not FK-enforced)
  cinema_id BIGINT UNSIGNED NULL,
  name VARCHAR(128) NOT NULL,
  description TEXT NULL,
  seat_rows INT UNSIGNED NULL,
  seat_cols INT UNSIGNED NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  -- legacy unique on (owner_id, name) removed in migrations; keep a non-unique composite if present
  KEY idx_owner_cinema_name (owner_id, cinema_id, name),
  KEY idx_halls_cinema_id (cinema_id, id),
  CONSTRAINT fk_halls_cinema FOREIGN KEY (cinema_id) REFERENCES cinemas(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- SEATS
CREATE TABLE IF NOT EXISTS seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  hall_id BIGINT UNSIGNED NOT NULL,
  row_label VARCHAR(16) NOT NULL,
  seat_number INT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_seats_hall_row_no (hall_id, row_label, seat_number),
  KEY idx_seats_hall (hall_id),
  CONSTRAINT fk_seats_hall FOREIGN KEY (hall_id) REFERENCES halls(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- SHOWS
CREATE TABLE IF NOT EXISTS shows (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  hall_id BIGINT UNSIGNED NOT NULL,
  movie_title VARCHAR(255) NOT NULL,
  starts_at DATETIME NOT NULL,
  ends_at DATETIME NOT NULL,
  price_cents INT UNSIGNED NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_shows_hall_starts (hall_id, starts_at),
  KEY idx_shows_hall_ends (hall_id, ends_at),
  CONSTRAINT fk_shows_hall FOREIGN KEY (hall_id) REFERENCES halls(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- SHOW_SEATS
CREATE TABLE IF NOT EXISTS show_seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  status ENUM('FREE','HELD','RESERVED') NOT NULL DEFAULT 'FREE',
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_showseats_show_seat (show_id, seat_id),
  KEY idx_showseats_show (show_id),
  KEY idx_showseats_seat (seat_id),
  CONSTRAINT fk_showseats_show FOREIGN KEY (show_id) REFERENCES shows(id)
    ON UPDATE CASCADE ON DELETE CASCADE,
  CONSTRAINT fk_showseats_seat FOREIGN KEY (seat_id) REFERENCES seats(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- SEAT_HOLDS
CREATE TABLE IF NOT EXISTS seat_holds (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  hold_token CHAR(64) NOT NULL,
  expires_at DATETIME NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_seat_holds_token (hold_token),
  UNIQUE KEY uk_seat_holds_active (show_id, seat_id),
  KEY idx_hold_show (show_id),
  KEY idx_hold_expires (expires_at),
  KEY idx_hold_show_expires (show_id, expires_at),
  CONSTRAINT fk_hold_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_hold_show FOREIGN KEY (show_id) REFERENCES shows(id) ON DELETE RESTRICT,
  CONSTRAINT fk_hold_seat FOREIGN KEY (seat_id) REFERENCES seats(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- RESERVATIONS
CREATE TABLE IF NOT EXISTS reservations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  show_id BIGINT UNSIGNED NOT NULL,
  status ENUM('PENDING','CONFIRMED','CANCELLED') NOT NULL DEFAULT 'PENDING',
  total_cents INT UNSIGNED NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_res_user (user_id),
  KEY idx_res_show (show_id),
  KEY idx_res_user_created (user_id, created_at),
  KEY idx_res_show_created (show_id, created_at),
  CONSTRAINT fk_res_user FOREIGN KEY (user_id) REFERENCES users(id)
    ON UPDATE CASCADE ON DELETE RESTRICT,
  CONSTRAINT fk_res_show FOREIGN KEY (show_id) REFERENCES shows(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- RESERVATION_SEATS
CREATE TABLE IF NOT EXISTS reservation_seats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  reservation_id BIGINT UNSIGNED NOT NULL,
  show_id BIGINT UNSIGNED NOT NULL,
  seat_id BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_res_seat_unique (show_id, seat_id),
  KEY idx_resseat_reservation (reservation_id),
  KEY idx_resseat_show (show_id),
  KEY idx_resseat_seat (seat_id),
  CONSTRAINT fk_resseat_reservation FOREIGN KEY (reservation_id) REFERENCES reservations(id)
    ON UPDATE CASCADE ON DELETE CASCADE,
  CONSTRAINT fk_resseat_show FOREIGN KEY (show_id) REFERENCES shows(id)
    ON UPDATE CASCADE ON DELETE RESTRICT,
  CONSTRAINT fk_resseat_seat FOREIGN KEY (seat_id) REFERENCES seats(id)
    ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- REFRESH_TOKENS
CREATE TABLE IF NOT EXISTS refresh_tokens (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  token_hash CHAR(64) NOT NULL,
  expires_at DATETIME NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_token_hash (token_hash),
  KEY idx_user_id (user_id),
  CONSTRAINT fk_refresh_user FOREIGN KEY (user_id) REFERENCES users(id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;
