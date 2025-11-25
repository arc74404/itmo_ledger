package data

import (
	"database/sql"
	"errors"
)

var (
	ErrRecordNotFound    = errors.New("record not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

type Models struct {
	BonusEntries BonusEntryModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		BonusEntries: BonusEntryModel{DB: db},
	}
}
