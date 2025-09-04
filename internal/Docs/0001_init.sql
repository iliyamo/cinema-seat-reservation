-- 0001_init.sql  (MySQL 8 / XAMPP)
-- Schema: users, refresh_tokens
-- Notes:
-- - Email is UNIQUE (collation is case-insensitive by default).
-- - Passwords are stored as bcrypt hashes (VARCHAR(60)).
-- - Refresh tokens: store ONLY the SHA-256 hash (hex, CHAR(64)) + JTI (UUID as CHAR(36)).
-- - ENGINE=InnoDB, CHARSET=utf8mb4, COLLATE=utf8mb4_general_ci for broad compatibility on XAMPP.

-- If you want to be explicit about database, uncomment next line:
-- USE cinema;

-- Users table
CREATE TABLE IF NOT EXISTS users (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  email          VARCHAR(255) NOT NULL,
  password_hash  VARCHAR(60)  NOT NULL,                           -- bcrypt hash
  role           ENUM('CUSTOMER','OWNER') NOT NULL DEFAULT 'CUSTOMER',
  is_active      TINYINT(1)   NOT NULL DEFAULT 1,
  created_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY users_email_uq (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- Refresh tokens (store ONLY hashed token)
CREATE TABLE IF NOT EXISTS refresh_tokens (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  token_id     CHAR(36)   NOT NULL,                                -- JTI (UUID string)
  token_hash   CHAR(64)   NOT NULL,                                -- SHA-256 hex of the refresh token
  expires_at   DATETIME   NOT NULL,
  revoked_at   DATETIME   NULL,
  created_at   TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY rt_token_id_uq (token_id),
  KEY idx_rt_user      (user_id),
  KEY idx_rt_valid     (user_id, revoked_at),
  CONSTRAINT fk_rt_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- Optional sanity checks (MySQL 8 supports CHECK but historically ignored; kept for documentation)
-- ALTER TABLE users ADD CONSTRAINT chk_role CHECK (role IN ('CUSTOMER','OWNER'));
