-- Cleanup script: reset stuck 'degraded' connections to 'ready'
-- 
-- Problem: Status 'degraded' was persisted to DB but excluded from scheduler
-- recovery query, causing accounts to get stuck forever after restart.
--
-- Fix: degraded is now only tracked in-memory (not persisted to DB).
-- This script cleans up existing stuck 'degraded' rows.

-- First, let's see how many are stuck
SELECT id, name, status, last_error, updated_at 
FROM connections 
WHERE status = 'degraded' AND is_active = 1;

-- Reset them to ready
UPDATE connections 
SET status = 'ready', 
    cooldown_until = NULL,
    last_error = NULL,
    last_error_code = NULL,
    updated_at = strftime('%s', 'now')
WHERE status = 'degraded' AND is_active = 1;

-- Verify
SELECT id, name, status 
FROM connections 
WHERE status = 'degraded' AND is_active = 1;
-- Should return 0 rows
