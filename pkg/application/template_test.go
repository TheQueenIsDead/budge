package application

import (
	"bytes"
	"html/template"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderTemplate executes a named template against the real template files, so that
// a field renamed in Go but not in the markup fails the build rather than the page.
func renderTemplate(t *testing.T, name string, data interface{}) string {
	t.Helper()

	tpl := template.New("").Funcs(templateFuncs())
	tpl, err := tpl.ParseGlob("../../web/templates/*.gohtml")
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, tpl.ExecuteTemplate(&buf, name, data))
	return buf.String()
}

func TestRenderAccounts(t *testing.T) {

	loan := account("loan", "Mortgage", "Kiwibank", "LOAN", -250000)
	loan.FormattedAccount = "38-9000-0000000-00"
	loan.Meta.LoanDetails.Interest.Rate = 6.35
	loan.Meta.LoanDetails.Interest.Type = "FIXED"
	loan.Meta.LoanDetails.Repayment.NextAmount = 1240
	loan.Meta.LoanDetails.Repayment.NextDate = time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)

	inactive := account("old", "Closed Card", "ANZ", "CREDITCARD", -40)
	inactive.Status = "INACTIVE"

	accounts := []models.Account{
		account("everyday", "Everyday", "Kiwibank", "CHECKING", 1100),
		loan,
		inactive,
	}
	transactions := []models.Transaction{transaction("everyday", 100)}

	refreshed, hasRefreshed := OldestRefresh(accounts)
	html := renderTemplate(t, "accounts", AccountsListProps{
		Portfolio:     BuildPortfolio(accounts),
		Groups:        BuildAccountGroups(accounts, transactions),
		LastRefreshed: refreshed,
		HasRefreshed:  hasRefreshed,
	})

	t.Run("shows the portfolio position", func(t *testing.T) {
		assert.Contains(t, html, "Net Worth")
		assert.Contains(t, html, "$-248,940.00") // 1100 - 250000 - 40
		assert.Contains(t, html, "$1,100.00")    // Assets
		assert.Contains(t, html, "Across 3 accounts")
	})

	t.Run("groups accounts under their connection", func(t *testing.T) {
		assert.Contains(t, html, "Kiwibank")
		assert.Contains(t, html, "ANZ")
	})

	t.Run("links each account to its detail page", func(t *testing.T) {
		assert.Contains(t, html, `href="/accounts/everyday"`)
		assert.Contains(t, html, `href="/accounts/loan"`)
	})

	t.Run("surfaces loan repayment details", func(t *testing.T) {
		assert.Contains(t, html, "6.35%")
		assert.Contains(t, html, "Fixed")
		assert.Contains(t, html, "Next $1,240.00")
		assert.Contains(t, html, "on 3 Sep")
	})

	t.Run("flags an account that is no longer active", func(t *testing.T) {
		assert.Contains(t, html, "INACTIVE")
	})

	t.Run("shows movement only for accounts with transactions", func(t *testing.T) {
		assert.Contains(t, html, "10.0%")       // Everyday moved 100 on a 1000 opening balance
		assert.Contains(t, html, "No activity") // The loan saw no transactions
	})
}

func TestRenderAccountsEmpty(t *testing.T) {
	html := renderTemplate(t, "accounts", AccountsListProps{
		Portfolio: BuildPortfolio(nil),
		Groups:    BuildAccountGroups(nil, nil),
	})

	assert.Contains(t, html, "No accounts yet")
	assert.NotContains(t, html, "Net Worth")
}
