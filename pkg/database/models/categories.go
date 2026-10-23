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
	// IncludeTransfers lets the rule match money moved between the owner's
	// own accounts, which rules otherwise leave alone. It exists for
	// payments that are real outgoings even though both ends are the
	// owner's, such as a mortgage repayment into a loan account.
	IncludeTransfers bool
}

// TypeCountedTransfer marks the outgoing leg of a transfer that a rule has
// chosen to count as spending. Every view leaves out "TRANSFER", so giving
// the leg its own type is what brings it into spend, everywhere at once.
const TypeCountedTransfer = "COUNTED TRANSFER"

// IsTransfer reports whether a transaction moves money between the owner's
// own accounts, whether or not a rule counts it.
func (t Transaction) IsTransfer() bool {
	return t.Type == "TRANSFER" || t.Type == TypeCountedTransfer
}

// Matches reports whether the rule applies to a transaction. Transfers are
// money moving between the owner's own accounts, so a rule only matches one
// when it opts in, and then only the leg where money leaves: the leg where it
// arrives stays a transfer, so the payment is counted once and never as
// income.
func (r CategoryRule) Matches(tx Transaction) bool {
	if r.Contains == "" {
		return false
	}
	if tx.IsTransfer() && (!r.IncludeTransfers || tx.Amount >= 0) {
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

// OverrideCategoryPrefix marks the category id of a transaction the owner
// filed by hand.
const OverrideCategoryPrefix = "override:"

// CategoryOverride files one transaction under a category, for the one-off
// payment no rule would describe, such as a plumber fixing the hot water
// cylinder. It wins over rules and over Akahu.
type CategoryOverride struct {
	TransactionID string
	Broad         string
	Specific      string
}

// CategorySettings holds the custom categories, the rules in the order they
// are tried, and the per-transaction overrides.
type CategorySettings struct {
	Categories []CustomCategory
	Rules      []CategoryRule
	Overrides  []CategoryOverride
}

// Override returns the override for a transaction, if it has one.
func (cs CategorySettings) Override(transactionID string) (CategoryOverride, bool) {
	for _, o := range cs.Overrides {
		if o.TransactionID == transactionID {
			return o, true
		}
	}
	return CategoryOverride{}, false
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

// Apply recategorises transactions in place. An override for the
// transaction wins; otherwise the first matching rule does. Only the copy
// being read is changed: Akahu's own category stays in the database, so
// removing an override or a rule restores it straight away.
func (cs CategorySettings) Apply(txs []Transaction) {
	if len(cs.Rules) == 0 && len(cs.Overrides) == 0 {
		return
	}
	overrides := make(map[string]CategoryOverride, len(cs.Overrides))
	for _, o := range cs.Overrides {
		overrides[o.TransactionID] = o
	}
	for i := range txs {
		if o, ok := overrides[txs[i].Id]; ok {
			file(&txs[i], OverrideCategoryPrefix+o.TransactionID, o.Broad, o.Specific)
			continue
		}
		if n := cs.MatchingRule(txs[i]); n >= 0 {
			rule := cs.Rules[n]
			file(&txs[i], RuleCategoryPrefix+rule.ID, rule.Broad, rule.Specific)
		}
	}
}

// file puts a transaction under a category. The ids mark where the category
// came from, and the group id is what Transaction.Categories checks before
// showing the group. Money leaving by transfer that is filed somewhere is
// counted as spending; the leg where it arrives stays a transfer.
func file(tx *Transaction, id, broad, specific string) {
	tx.Category.Id = id
	tx.Category.Name = specific
	tx.Category.Groups.PersonalFinance.Id = id
	tx.Category.Groups.PersonalFinance.Name = broad
	if tx.IsTransfer() && tx.Amount < 0 {
		tx.Type = TypeCountedTransfer
	}
}
