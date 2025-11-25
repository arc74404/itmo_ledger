package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"time"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/validator"
)

type transactionIn struct {
	UserId       string `json:"user_id"`
	Amount       int    `json:"amount"`
	Type         string `json:"type"`
	LifetimeDays *int   `json:"lifetime_days,omitempty"`
}

type balanceResponse struct {
	UserId   uuid.UUID      `json:"user_id"`
	Balance  int            `json:"balance"`
	Expiring map[string]int `json:"expiring"`
}

func (app *application) createTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var trxIn transactionIn
	err := app.readJSON(w, r, &trxIn)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	userId, err := uuid.Parse(trxIn.UserId)

	v := validator.New()
	v.Check(err == nil, "user_id", "must be uuid")
	v.Check(trxIn.Amount > 0, "amount", "must be positive")
	v.Check(validator.IsPermitted(trxIn.Type, "deposit", "withdrawal"), "type", "must be deposit or withdrawal")

	// Проверка lifetime_days, если указан
	if trxIn.LifetimeDays != nil {
		v.Check(*trxIn.LifetimeDays > 0, "lifetime_days", "must be positive")
	}

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	lifetimeDays := 30
	if trxIn.LifetimeDays != nil {
		lifetimeDays = *trxIn.LifetimeDays
	}

	// Начинаем транзакцию
	tx, err := app.db.Begin()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	defer tx.Rollback()
	// Skip balance table check - bonus entries system doesn't require user pre-registration

	if trxIn.Type == "deposit" {
		err = app.handleDeposit(tx, userId, trxIn.Amount, lifetimeDays)
	} else {
		err = app.handleWithdrawal(tx, userId, trxIn.Amount)
	}

	if err != nil {
		if errors.Is(err, data.ErrInsufficientFunds) {
			app.badRequestResponse(w, r, err)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// Коммитим транзакцию
	if err = tx.Commit(); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Получаем обновленный баланс для ответа
	balance, err := app.models.BonusEntries.GetTotalBalance(userId)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	response := map[string]interface{}{
		"user_id": userId,
		"amount":  trxIn.Amount,
		"type":    trxIn.Type,
		"balance": balance,
	}

	if err = app.writeJSON(w, http.StatusOK, response, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

func (app *application) handleDeposit(tx *sql.Tx, userId uuid.UUID, amount int, lifetimeDays int) error {

	now := time.Now()
	entry := &data.BonusEntry{
		Id:           uuid.New(),
		UserId:       userId,
		Amount:       amount,
		CreatedAt:    now,
		LifetimeDays: lifetimeDays,
		Status:       data.BonusEntryStatusActive,
	}

	expiresAt := entry.ExpiresAt()

	query := `
		INSERT INTO bonus_entries (id, user_id, amount, created_at, expires_at, lifetime_days, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := tx.QueryRowContext(ctx, query,
		entry.Id,
		entry.UserId,
		entry.Amount,
		entry.CreatedAt,
		expiresAt,
		entry.LifetimeDays,
		entry.Status,
	).Scan(&entry.Id, &entry.CreatedAt)

	return err
}

func (app *application) handleWithdrawal(tx *sql.Tx, userId uuid.UUID, amount int) error {
	// Используем метод модели для списания с блокировками
	_, err := app.models.BonusEntries.SpendEntries(tx, userId, amount)
	return err
}

func (app *application) showUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userId, err := app.readIDParam(r)
	if err != nil || userId == uuid.Nil {
		app.notFoundResponse(w, r)
		return
	}

	// Skip balance table check - bonus entries system allows checking balance for any user

	// Получаем общий баланс
	balance, err := app.models.BonusEntries.GetTotalBalance(userId)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Получаем информацию о сгорании баллов (на ближайшие 7 дней)
	expiring, err := app.models.BonusEntries.GetExpiringEntries(userId, 7)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	response := balanceResponse{
		UserId:   userId,
		Balance:  balance,
		Expiring: expiring,
	}

	if err = app.writeJSON(w, http.StatusOK, response, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
