CREATE TABLE IF NOT EXISTS balances (
    id uuid PRIMARY KEY,
    updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW()
);
