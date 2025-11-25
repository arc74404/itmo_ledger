-- Recreate the balances table and foreign key constraint (rollback migration)
CREATE TABLE IF NOT EXISTS balances (
    id uuid PRIMARY KEY,
    updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    amount int
);

-- Recreate the foreign key constraint
ALTER TABLE bonus_entries
ADD CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES balances(id) ON DELETE CASCADE;
