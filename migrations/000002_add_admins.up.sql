-- Migration: 000002_add_admins.up.sql
-- Ops dashboard admin accounts (separate from drivers and passengers).
-- Admins are seeded here; they cannot self-register via OTP.

CREATE TABLE admins (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone      VARCHAR(20) UNIQUE NOT NULL,
    name       VARCHAR(100) NOT NULL DEFAULT 'Admin',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_admins_phone ON admins(phone);

-- Seed the default admin. Change the phone to your own number.
INSERT INTO admins (phone, name) VALUES ('+6281211571997', 'Admin');
