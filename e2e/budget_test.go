//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp/kb"
	"github.com/stretchr/testify/assert"
)

// TestBudgetTarget sets a target on a category with none. Its row shows the
// spending against it, the period it belongs to picks it up, and it is saved.
func TestBudgetTarget(t *testing.T) {
	p := open(t)
	p.visit("/budget")

	row := `.b-target:has(input[name=category][value="eating-out"])`
	p.click(row + " summary")
	p.click(row + " input[name=target_amount]")
	p.typeText("40")
	p.press(kb.Tab)
	p.settle()

	p.eventually(p.contains(row, "of $40"))
	// Each period card names the categories it covers in its title.
	p.eventually(func() error {
		covered := eval[string](p, `document.querySelector('#budget-periods .b-period:has(#period-weekly) [title]')?.title ?? ''`)
		if !strings.Contains(covered, "Eating & drinking out") {
			return fmt.Errorf("the weekly period covers %q", covered)
		}
		return nil
	})

	p.visit("/budget")
	assert.Contains(t, p.text(row), "of $40", "the target is saved")
}

// TestIncomeKeepsTheSavingsGoal sets a savings goal, then changes the salary.
// The two are saved from separate forms into one record, and saving the salary
// once wiped the goal.
func TestIncomeKeepsTheSavingsGoal(t *testing.T) {
	p := open(t)
	p.visit("/budget/income")

	p.click("input[name=savings_goal]")
	p.typeText("150")
	p.press(kb.Tab)
	p.settle()

	p.click("#salaryAmount")
	p.selectAll("#salaryAmount")
	p.typeText("90000")
	p.press(kb.Tab)
	p.settle()

	p.visit("/budget/income")
	assert.Equal(t, "90000.00", p.value("#salaryAmount"), "the salary is saved")
	assert.Equal(t, "150.00", p.value("input[name=savings_goal]"), "the savings goal is kept")
}
