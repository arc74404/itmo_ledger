-- Создание типа для статуса записи баллов
CREATE TYPE bonus_entry_status AS ENUM ('active', 'expired', 'spent');

-- Создание таблицы для хранения отдельных начислений баллов
CREATE TABLE IF NOT EXISTS bonus_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL,
    amount int NOT NULL CHECK (amount > 0),
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    expires_at timestamp(0) with time zone NOT NULL,
    lifetime_days int NOT NULL CHECK (lifetime_days > 0),
    status bonus_entry_status NOT NULL DEFAULT 'active',
    spent_at timestamp(0) with time zone,
    CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES balances(id) ON DELETE CASCADE,
    CONSTRAINT chk_expires_after_created CHECK (expires_at > created_at)
);

-- Индекс для быстрого поиска активных баллов пользователя
CREATE INDEX idx_bonus_entries_user_active ON bonus_entries(user_id, status) 
    WHERE status = 'active';

-- Индекс для поиска баллов по дате сгорания (для автоматического сгорания)
CREATE INDEX idx_bonus_entries_expires_at ON bonus_entries(expires_at, status) 
    WHERE status = 'active';

-- Индекс для поиска всех баллов пользователя (для расчета баланса)
CREATE INDEX idx_bonus_entries_user_id ON bonus_entries(user_id);

-- Композитный индекс для эффективного поиска активных баллов пользователя с сортировкой по дате создания (FIFO)
CREATE INDEX idx_bonus_entries_user_active_created ON bonus_entries(user_id, created_at) 
    WHERE status = 'active';

