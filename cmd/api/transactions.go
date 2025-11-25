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

var (
	errNoBalanceToMultiply     = errors.New("no active balance to multiply")
	errZeroBonusAfterMultiply  = errors.New("multiply percent too small for current balance")
	errMultiplyPercentTooLarge = errors.New("multiply percent too large")
)

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
	v.Check(validator.IsPermitted(trxIn.Type, "deposit", "withdrawal", "multiply_percent"), "type", "must be deposit, withdrawal or multiply_percent")

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

	var processedAmount int

	switch trxIn.Type {
	case "deposit":
		err = app.handleDeposit(tx, userId, trxIn.Amount, lifetimeDays)
		processedAmount = trxIn.Amount
	case "withdrawal":
		err = app.handleWithdrawal(tx, userId, trxIn.Amount)
		processedAmount = trxIn.Amount
	case "multiply":
		processedAmount, err = app.handleMultiply(tx, userId, trxIn.Amount, lifetimeDays)
	}

	if err != nil {
		switch {
		case errors.Is(err, data.ErrInsufficientFunds), errors.Is(err, errNoBalanceToMultiply),
			errors.Is(err, errZeroBonusAfterMultiply), errors.Is(err, errMultiplyPercentTooLarge):
			app.badRequestResponse(w, r, err)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	//commit transaction
	if err = tx.Commit(); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// get updated balance for response
	balance, err := app.models.BonusEntries.GetTotalBalance(userId)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	amountForResponse := trxIn.Amount
	if processedAmount > 0 {
		amountForResponse = processedAmount
	}

	response := map[string]interface{}{
		"user_id": userId,
		"amount":  amountForResponse,
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
	_, err := app.models.BonusEntries.SpendEntries(tx, userId, amount)
	return err
}

func (app *application) handleMultiply(tx *sql.Tx, userId uuid.UUID, percent int, lifetimeDays int) (int, error) {
	if percent <= 0 {
		return 0, errZeroBonusAfterMultiply
	}
	if percent > 200 {
		return 0, errMultiplyPercentTooLarge
	}

	entries, err := app.models.BonusEntries.GetActiveEntriesForUpdate(tx, userId)
	if err != nil {
		return 0, err
	}

	total := 0
	for _, entry := range entries {
		total += entry.Amount
	}

	if total <= 0 {
		return 0, errNoBalanceToMultiply
	}

	bonus := int((int64(total) * int64(percent)) / 100)
	if bonus <= 0 {
		return 0, errZeroBonusAfterMultiply
	}

	if err := app.handleDeposit(tx, userId, bonus, lifetimeDays); err != nil {
		return 0, err
	}

	return bonus, nil
}

func (app *application) showUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userId, err := app.readIDParam(r)
	if err != nil || userId == uuid.Nil {
		app.notFoundResponse(w, r)
		return
	}

	// Skip balance table check - bonus entries system allows checking balance for any user

	// ПTake total balance
	balance, err := app.models.BonusEntries.GetTotalBalance(userId)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Get expiring entries (next 7 days)
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
