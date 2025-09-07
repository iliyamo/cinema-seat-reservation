-- 0007_reservation_seats.down.sql
-- Roll back the creation of the reservation_seats table.
-- This migration drops the reservation_seats table if it exists.

DROP TABLE IF EXISTS reservation_seats;