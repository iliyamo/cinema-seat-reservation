-- 0002_roles.up.sql
-- Migrate from users.role (ENUM) to roles table + users.role_id (FK)

-- Note: Some DDL in MySQL causes implicit commits. Run this once.

-- 1) Roles table
CREATE TABLE IF NOT EXISTS roles (
  id   TINYINT UNSIGNED NOT NULL PRIMARY KEY,
  name VARCHAR(32) NOT NULL UNIQUE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- 2) Seed roles
INSERT INTO roles (id, name) VALUES
  (1, 'CUSTOMER'),
  (2, 'OWNER')
ON DUPLICATE KEY UPDATE name = VALUES(name);

-- 3) Add role_id column to users (temporary NULL allowed)
ALTER TABLE users
  ADD COLUMN role_id TINYINT UNSIGNED NULL;

-- 4) Backfill role_id from old ENUM column
UPDATE users
SET role_id = CASE role
  WHEN 'CUSTOMER' THEN 1
  WHEN 'OWNER'    THEN 2
  ELSE 1
END
WHERE role_id IS NULL;

-- 5) Add FK and then enforce NOT NULL
ALTER TABLE users
  ADD CONSTRAINT fk_users_role FOREIGN KEY (role_id) REFERENCES roles(id);

-- Ensure no NULLs remain, then make NOT NULL
UPDATE users SET role_id = 1 WHERE role_id IS NULL;

ALTER TABLE users
  MODIFY role_id TINYINT UNSIGNED NOT NULL;

-- 6) Drop old ENUM column
ALTER TABLE users
  DROP COLUMN role;
