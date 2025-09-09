-- 0011_indexes.down.sql
-- Drop the indexes introduced in 0011_indexes.up.sql

ALTER TABLE halls
  DROP INDEX idx_halls_cinema_id;

ALTER TABLE reservations
  DROP INDEX idx_res_user_created,
  DROP INDEX idx_res_show_created;

ALTER TABLE seat_holds
  DROP INDEX idx_hold_show_expires;

ALTER TABLE shows
  DROP INDEX idx_shows_hall_ends;
