package models

import (
	"math"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// CustomCategory is a category of the owner's own, from before budge had its
// own categories. Rules named them; they are only read now, so a rule or an
// older backup that names one still lands somewhere.
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
	Type string
	// Category is the budge category or subcategory the rule files under.
	Category string
	// Broad and Specific are what the rule filed under before budge had its
	// own categories, read by Target when Category is empty.
	Broad    string
	Specific string
	// IncludeTransfers lets the rule match money moved between the owner's
	// own accounts, which rules otherwise leave alone. It exists for
	// payments that are real outgoings even though both ends are the
	// owner's, such as a mortgage repayment into a loan account.
	IncludeTransfers bool
	// AtLeast and AtMost narrow the rule to amounts in a range, both ends
	// included, for payments the text cannot tell apart, such as two policies
	// paid to the same insurer. They are sizes, whether money went out or
	// came in, and zero leaves that end open.
	AtLeast float64
	AtMost  float64
}

// InRange reports whether a transaction's size is within the rule's amounts.
func (r CategoryRule) InRange(tx Transaction) bool {
	amount := math.Abs(tx.Amount)
	return (r.AtLeast <= 0 || amount >= r.AtLeast) && (r.AtMost <= 0 || amount <= r.AtMost)
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
	if !r.InRange(tx) {
		return false
	}
	needle := strings.ToLower(r.Contains)
	return strings.Contains(strings.ToLower(tx.Description), needle) ||
		strings.Contains(strings.ToLower(tx.Merchant.Name), needle)
}

// What filed a transaction, when it was not Akahu.
const (
	FiledByOwner = "owner"
	FiledByRule  = "rule"
)

// CategoryOverride files one transaction under a category, for the one-off
// payment no rule would describe, such as a plumber fixing the hot water
// cylinder. It wins over rules and over Akahu.
type CategoryOverride struct {
	TransactionID string
	Category      string
	// Broad and Specific are from before budge had its own categories.
	Broad    string
	Specific string
}

// Target is the budge category the rule files under. A rule saved before
// budge had its own categories names one of Akahu's, which is placed the
// way Akahu's own category would be.
func (r CategoryRule) Target() string {
	return target(r.Category, r.Broad, r.Specific)
}

// Target is the budge category the override files under.
func (o CategoryOverride) Target() string {
	return target(o.Category, o.Broad, o.Specific)
}

// target is where a rule or override files today: its category, or the one
// that category's transactions went to if it was deleted.
func target(category, broad, specific string) string {
	if found, ok := BudgetCategoryByKey(category); ok {
		return found.Key
	}
	if broad == "" {
		broad = specific
	}
	return ResolveBudgetCategory(AkahuBudgetCategory(broad, specific, ""))
}

// CategorySettings holds the rules in the order they are tried, the
// per-transaction overrides, and any custom categories from before budge
// had its own.
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

// Apply files each transaction under a budge category: an override for it
// wins, then the first matching rule, then Akahu's category. Akahu's own
// category is left as it came, so removing an override or a rule puts it back
// straight away.
func (cs CategorySettings) Apply(txs []Transaction) {
	overrides := make(map[string]CategoryOverride, len(cs.Overrides))
	for _, o := range cs.Overrides {
		overrides[o.TransactionID] = o
	}
	for i := range txs {
		if o, ok := overrides[txs[i].Id]; ok {
			file(&txs[i], o.Target(), FiledByOwner)
			continue
		}
		if n := cs.MatchingRule(txs[i]); n >= 0 {
			file(&txs[i], cs.Rules[n].Target(), FiledByRule)
			continue
		}
		txs[i].Filed = txs[i].BudgetCategory()
		txs[i].FiledBy = ""
	}
}

// file puts a transaction under a category. Money leaving by transfer that is
// filed somewhere is counted as spending; the leg where it arrives stays a
// transfer.
func file(tx *Transaction, category, by string) {
	tx.Filed = category
	tx.FiledBy = by
	if tx.IsTransfer() && tx.Amount < 0 {
		tx.Type = TypeCountedTransfer
	}
}
