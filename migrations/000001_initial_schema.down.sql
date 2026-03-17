-- Rollback: 000001_initial_schema
DROP TABLE IF EXISTS fare_configs;
DROP TABLE IF EXISTS ratings;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS eta_corrections;
DROP TABLE IF EXISTS trip_locations;
DROP TABLE IF EXISTS trips;
DROP TABLE IF EXISTS drivers;
DROP TABLE IF EXISTS users;
DROP EXTENSION IF EXISTS "pgcrypto";
