package application

import (
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
)

func TestBalanceCardLeavesOutLoans(t *testing.T) {
	everyday := models.Account{Id: "everyday", Type: "CHECKING"}
	everyday.Balance.Current = 1500
	loan := models.Account{Id: "loan", Type: "LOAN"}
	loan.Balance.Current = -5456.28

	spent := models.Transaction{Account: "everyday", Amount: -500}
	interest := models.Transaction{Account: "loan", Amount: -40}

	balance, _, _, _ := BuildCards([]models.Account{everyday, loan},
		[]models.Transaction{spent}, []models.Transaction{spent, interest})

	assert.Equal(t, 1500.0, balance.Total, "a loan is owed, not held")
	assert.Equal(t, 2000.0, balance.PreviousTotal, "and its own entries do not move last month's figure")
}
