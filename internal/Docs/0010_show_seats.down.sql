-- Rollback for 0009_show_seats.up.sql
-- Drop show_seats table if it exists.  This migration reverses the
-- creation of the show_seats table.

DROP TABLE IF EXISTS show_seats;