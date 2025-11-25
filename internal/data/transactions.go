package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Balance struct {
	Id        uuid.UUID `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BalanceModel struct {
	DB *sql.DB
}

func (m BalanceModel) Insert(balance *Balance) error {
	query := `
		INSERT INTO balances (id)
		VALUES ($1)
		RETURNING id, updated_at`
	args := []any{balance.Id}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, args...).Scan(&balance.Id, &balance.UpdatedAt)
}

func (m BalanceModel) Get(id uuid.UUID) (*Balance, error) {
	balance := new(Balance)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT id, updated_at
		FROM balances
		WHERE id = $1`
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&balance.Id,
		&balance.UpdatedAt,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}

	return balance, nil
}

func (m BalanceModel) Update(balance *Balance) error {
	query := `
		UPDATE balances
		SET updated_at = $2
		WHERE id = $1
		RETURNING updated_at`
	args := []any{
		balance.Id,
		time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&balance.UpdatedAt)
	if err != nil {
		return err
	}
	return nil
}

// GetOrCreate получает баланс пользователя или создает новый, если его нет
func (m BalanceModel) GetOrCreate(id uuid.UUID) (*Balance, error) {
	balance, err := m.Get(id)
	if err == nil {
		return balance, nil
	}

	if !errors.Is(err, ErrRecordNotFound) {
		return nil, err
	}

	// Создаем новый баланс
	newBalance := &Balance{
		Id: id,
	}
	err = m.Insert(newBalance)
	if err != nil {
		return nil, err
	}

	return newBalance, nil
}
