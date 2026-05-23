-- ─────────────────────────────────────────────────────────────────────────────
-- Initial database setup — runs once when Postgres container first starts
-- ─────────────────────────────────────────────────────────────────────────────

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Enable pg_trgm for fast text search (useful for phone/email lookups)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Set timezone
SET timezone = 'Asia/Kolkata';

-- Create indexes (GORM creates basic ones; we add composite ones here)
-- These run AFTER GORM AutoMigrate completes (app starts first)
-- In production you'd run these as separate migrations using golang-migrate
