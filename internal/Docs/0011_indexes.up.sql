-- 0011_indexes.up.sql
-- Add targeted performance indexes aligned with query patterns in the Docs.
-- Compatible with MySQL 5.7+/8.0+ and MariaDB 10.4+.

-- 1) Halls: list/filter by cinema quickly; keep natural PK order for stable pagination.
ALTER TABLE halls
  ADD KEY idx_halls_cinema_id (cinema_id, id);

-- 2) Reservations: history per user ordered by created_at; and per show.
ALTER TABLE reservations
  ADD KEY idx_res_user_created (user_id, created_at),
  ADD KEY idx_res_show_created (show_id, created_at);

-- 3) Seat holds: find valid/expired holds per show efficiently.
ALTER TABLE seat_holds
  ADD KEY idx_hold_show_expires (show_id, expires_at);

-- 4) Shows: help with overlap/time-range checks alongside existing (hall_id, starts_at).
ALTER TABLE shows
  ADD KEY idx_shows_hall_ends (hall_id, ends_at);
