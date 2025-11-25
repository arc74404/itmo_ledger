package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type BonusEntryStatus string

const (
	BonusEntryStatusActive  BonusEntryStatus = "active"
	BonusEntryStatusExpired BonusEntryStatus = "expired"
	BonusEntryStatusSpent   BonusEntryStatus = "spent"
)

type BonusEntry struct {
	Id           uuid.UUID        `json:"id"`
	UserId       uuid.UUID        `json:"user_id"`
	Amount       int              `json:"amount"`
	CreatedAt    time.Time        `json:"created_at"`
	LifetimeDays int              `json:"lifetime_days"`
	Status       BonusEntryStatus `json:"status"`
	SpentAt      *time.Time       `json:"spent_at,omitempty"`
}

// ExpiresAt calculates the expiration date based on CreatedAt and LifetimeDays
func (e *BonusEntry) ExpiresAt() time.Time {
	return e.CreatedAt.AddDate(0, 0, e.LifetimeDays)
}

type BonusEntryModel struct {
	DB *sql.DB
}

// Insert creates a new entry for the bonus entry
func (m BonusEntryModel) Insert(entry *BonusEntry) error {
	expiresAt := entry.ExpiresAt()
	query := `
		INSERT INTO bonus_entries (id, user_id, amount, created_at, expires_at, lifetime_days, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	args := []any{
		entry.Id,
		entry.UserId,
		entry.Amount,
		entry.CreatedAt,
		expiresAt,
		entry.LifetimeDays,
		entry.Status,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(
		&entry.Id,
		&entry.CreatedAt,
	)
	if err != nil {
		return err
	}

	return nil
}

// GetActiveEntries возвращает все активные записи баллов пользователя, отсортированные по дате создания (FIFO)
func (m BonusEntryModel) GetActiveEntries(userId uuid.UUID) ([]*BonusEntry, error) {
	query := `
		SELECT id, user_id, amount, created_at, lifetime_days, status, spent_at
		FROM bonus_entries
		WHERE user_id = $1 
			AND status = 'active' 
			AND expires_at > NOW()
		ORDER BY created_at ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, userId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*BonusEntry
	for rows.Next() {
		var entry BonusEntry
		err := rows.Scan(
			&entry.Id,
			&entry.UserId,
			&entry.Amount,
			&entry.CreatedAt,
			&entry.LifetimeDays,
			&entry.Status,
			&entry.SpentAt,
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// GetActiveEntriesForUpdate returns active entries with lock for transactions (SELECT FOR UPDATE)
func (m BonusEntryModel) GetActiveEntriesForUpdate(tx *sql.Tx, userId uuid.UUID) ([]*BonusEntry, error) {
	query := `
		SELECT id, user_id, amount, created_at, lifetime_days, status, spent_at
		FROM bonus_entries
		WHERE user_id = $1 
			AND status = 'active' 
			AND expires_at > NOW()
		ORDER BY created_at ASC
		FOR UPDATE`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := tx.QueryContext(ctx, query, userId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*BonusEntry
	for rows.Next() {
		var entry BonusEntry
		err := rows.Scan(
			&entry.Id,
			&entry.UserId,
			&entry.Amount,
			&entry.CreatedAt,
			&entry.LifetimeDays,
			&entry.Status,
			&entry.SpentAt,
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// SpendEntries spends entries by FIFO principle within a transaction
// Returns a list of entries that were used for spending
func (m BonusEntryModel) SpendEntries(tx *sql.Tx, userId uuid.UUID, amount int) ([]*BonusEntry, error) {
	// Get active entries with lock for transactions
	entries, err := m.GetActiveEntriesForUpdate(tx, userId)
	if err != nil {
		return nil, err
	}

	// Calculate available balance
	availableBalance := 0
	for _, entry := range entries {
		availableBalance += entry.Amount
	}

	if availableBalance < amount {
		return nil, ErrInsufficientFunds
	}

	// Spend by FIFO principle
	remainingAmount := amount
	var spentEntries []*BonusEntry
	now := time.Now()

	for _, entry := range entries {
		if remainingAmount <= 0 {
			break
		}

		spentAmount := entry.Amount
		if remainingAmount < entry.Amount {
			spentAmount = remainingAmount
			// Partial spending - create a new entry with the remainder
			remainingEntry := &BonusEntry{
				Id:           uuid.New(),
				UserId:       entry.UserId,
				Amount:       entry.Amount - spentAmount,
				CreatedAt:    entry.CreatedAt,
				LifetimeDays: entry.LifetimeDays,
				Status:       BonusEntryStatusActive,
			}

			insertQuery := `
				INSERT INTO bonus_entries (id, user_id, amount, created_at, expires_at, lifetime_days, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, err := tx.ExecContext(ctx, insertQuery,
				remainingEntry.Id,
				remainingEntry.UserId,
				remainingEntry.Amount,
				remainingEntry.CreatedAt,
				remainingEntry.ExpiresAt(),
				remainingEntry.LifetimeDays,
				remainingEntry.Status,
			)
			cancel()
			if err != nil {
				return nil, err
			}
		}

		// Update status of the entry to 'spent'
		updateQuery := `
			UPDATE bonus_entries
			SET status = 'spent', spent_at = $1, amount = $2
			WHERE id = $3`

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := tx.ExecContext(ctx, updateQuery, now, spentAmount, entry.Id)
		cancel()
		if err != nil {
			return nil, err
		}

		entry.Status = BonusEntryStatusSpent
		entry.SpentAt = &now
		entry.Amount = spentAmount
		spentEntries = append(spentEntries, entry)

		remainingAmount -= spentAmount
	}

	return spentEntries, nil
}

// GetTotalBalance calculates the total balance of active bonus entries for a user
func (m BonusEntryModel) GetTotalBalance(userId uuid.UUID) (int, error) {
	query := `
		SELECT COALESCE(SUM(amount), 0)
		FROM bonus_entries
		WHERE user_id = $1 
			AND status = 'active' 
			AND expires_at > NOW()`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var balance int
	err := m.DB.QueryRowContext(ctx, query, userId).Scan(&balance)
	if err != nil {
		return 0, err
	}

	return balance, nil
}

// GetExpiringEntries returns information about entries that will expire in the next days
// days - number of days for analysis
func (m BonusEntryModel) GetExpiringEntries(userId uuid.UUID, days int) (map[string]int, error) {
	query := `
		SELECT 
			DATE(expires_at) as expire_date,
			SUM(amount) as total_amount
		FROM bonus_entries
		WHERE user_id = $1 
			AND status = 'active' 
			AND expires_at > NOW()
			AND expires_at <= NOW() + INTERVAL '1 day' * $2
		GROUP BY DATE(expires_at)
		ORDER BY expire_date ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, userId, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var expireDate time.Time
		var totalAmount int
		err := rows.Scan(&expireDate, &totalAmount)
		if err != nil {
			return nil, err
		}
		result[expireDate.Format("2006-01-02")] = totalAmount
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// UpdateExpiredEntries updates the status of expired entries to 'expired'
func (m BonusEntryModel) UpdateExpiredEntries() (int64, error) {
	query := `
		UPDATE bonus_entries
		SET status = 'expired'
		WHERE status = 'active' 
			AND expires_at <= NOW()`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}
