-- Remove foreign key constraint from bonus_entries table
ALTER TABLE bonus_entries DROP CONSTRAINT IF EXISTS fk_user;

-- Drop the balances table and related constraints
DROP TABLE IF EXISTS balances;