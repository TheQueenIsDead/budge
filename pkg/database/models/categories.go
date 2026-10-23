package models

import (
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// CustomCategory is a category of the owner's own, alongside the ones Akahu
// assigns, for rules to file transactions under.
type CustomCategory struct {
	ID       string
	Broad    string
	Specific string
}

// CategoryRule files matching transactions under a category, overriding the
// one Akahu gave them.
type CategoryRule struct {
	ID string
	// Contains is matched case-insensitively against the description and the
	// merchant name.
	Contains string
	// Type narrows the rule to one transaction type, such as LOAN. Empty
	// matches any type.
	Type     string
	Broad    string
	Specific string
}

// Matches reports whether the rule applies to a transaction. Transfers are
// money moving between the owner's own accounts, so no rule recategorises
// them.
func (r CategoryRule) Matches(tx Transaction) bool {
	if r.Contains == "" || tx.Type == "TRANSFER" {
		return false
	}
	if r.Type != "" && !strings.EqualFold(r.Type, tx.Type) {
		return false
	}
	needle := strings.ToLower(r.Contains)
	return strings.Contains(strings.ToLower(tx.Description), needle) ||
		strings.Contains(strings.ToLower(tx.Merchant.Name), needle)
}

// RuleCategoryPrefix starts the category id of a transaction a rule filed.
const RuleCategoryPrefix = "rule:"

// CategorySettings holds the custom categories and the rules, in the order
// the rules are tried.
type CategorySettings struct {
	Categories []CustomCategory
	Rules      []CategoryRule
}

func (cs CategorySettings) Key() []byte    { return []byte("categories") }
func (cs CategorySettings) Bucket() []byte { return buckets.SettingsBucket }

// MatchingRule returns the index of the first rule that matches, or -1.
func (cs CategorySettings) MatchingRule(tx Transaction) int {
	for i, rule := range cs.Rules {
		if rule.Matches(tx) {
			return i
		}
	}
	return -1
}

// Apply recategorises transactions in place. The first matching rule wins.
// Only the copy being read is changed: Akahu's own category stays in the
// database, so deleting a rule restores it straight away.
func (cs CategorySettings) Apply(txs []Transaction) {
	if len(cs.Rules) == 0 {
		return
	}
	for i := range txs {
		if n := cs.MatchingRule(txs[i]); n >= 0 {
			rule := cs.Rules[n]
			// The ids mark the category as set by a rule, and the group id is
			// what Transaction.Categories checks before showing the group.
			txs[i].Category.Id = RuleCategoryPrefix + rule.ID
			txs[i].Category.Name = rule.Specific
			txs[i].Category.Groups.PersonalFinance.Id = RuleCategoryPrefix + rule.ID
			txs[i].Category.Groups.PersonalFinance.Name = rule.Broad
		}
	}
}
