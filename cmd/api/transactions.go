package main

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/validator"
)

type transactionIn struct {
	UserId string `json:"user_id"`
	Amount int    `json:"amount"`
	Type   string `json:"type"`
}

func (app *application) createTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var trxIn transactionIn
	err := app.readJSON(w, r, &trxIn)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	id, err := uuid.Parse(trxIn.UserId)

	v := validator.New()
	v.Check(err == nil, "user_id", "must be uuid")
	v.Check(trxIn.Amount > 0, "amount", "must be positive")
	v.Check(validator.IsPermitted(trxIn.Type, "deposit", "withdrawal"), "type", "must be deposit or withdrawal")

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	balance, err := app.models.Balances.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.createNewBalance(w, r, &data.Balance{
				Id:     id,
				Amount: trxIn.Amount,
			})
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.updateBalance(w, r, balance, trxIn)
}

func (app *application) createNewBalance(w http.ResponseWriter, r *http.Request, balance *data.Balance) {
	err := app.models.Balances.Insert(balance)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
	err = app.writeJSON(w, http.StatusCreated, balance, nil)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

func (app *application) updateBalance(w http.ResponseWriter, r *http.Request, balance *data.Balance, trxId transactionIn) {
	if trxId.Type == "withdrawal" && balance.Amount < trxId.Amount {
		app.badRequestResponse(w, r, errors.New("insufficient funds"))
		return
	}

	if trxId.Type == "deposit" {
		balance.Amount += trxId.Amount
	} else {
		balance.Amount -= trxId.Amount
	}
	err := app.models.Balances.Update(balance)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
	err = app.writeJSON(w, http.StatusOK, balance, nil)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

func (app *application) showUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil || id == uuid.Nil {
		app.notFoundResponse(w, r)
		return
	}

	balance, err := app.models.Balances.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	if err = app.writeJSON(w, http.StatusOK, balance, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
