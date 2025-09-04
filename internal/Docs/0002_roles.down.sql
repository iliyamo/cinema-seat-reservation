-- 0002_roles.down.sql
-- Roll back to users.role (ENUM) and drop roles table

-- 1) Re-create ENUM column on users
ALTER TABLE users
  ADD COLUMN role ENUM('CUSTOMER','OWNER') NOT NULL DEFAULT 'CUSTOMER';

-- 2) Backfill ENUM role from role_id/roles.name
UPDATE users u
JOIN roles r ON r.id = u.role_id
SET u.role = r.name;

-- 3) Drop FK and role_id column
ALTER TABLE users
  DROP FOREIGN KEY fk_users_role;

ALTER TABLE users
  DROP COLUMN role_id;

-- 4) Drop roles table
DROP TABLE IF EXISTS roles;
