package models

import (
	"encoding/json"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// MemoryKind is what a memory was: a trip, a goal reached, a big purchase. It
// is a fixed set rather than free text so every memory of a kind wears the same
// colour and icon on the timeline.
type MemoryKind struct {
	Key    string
	Label  string
	Icon   string
	Accent string
}

// MemoryKinds is the ordered registry of kinds, in the order the picker offers
// them.
var MemoryKinds = []MemoryKind{
	{Key: "trip", Label: "Trip", Icon: "bi-airplane", Accent: "slate"},
	{Key: "goal", Label: "Goal reached", Icon: "bi-trophy", Accent: "sage"},
	{Key: "purchase", Label: "Big purchase", Icon: "bi-bag-heart", Accent: "clay"},
	{Key: "experience", Label: "Experience", Icon: "bi-stars", Accent: "plum"},
	{Key: "home", Label: "Home", Icon: "bi-house-heart", Accent: "brick"},
	{Key: "other", Label: "Moment", Icon: "bi-bookmark-star", Accent: "muted"},
}

// MemoryKindByKey looks up a kind, falling back to the catch-all so a stored
// key that is later retired still renders as something.
func MemoryKindByKey(key string) MemoryKind {
	for _, kind := range MemoryKinds {
		if kind.Key == key {
			return kind
		}
	}
	return MemoryKinds[len(MemoryKinds)-1]
}

// Memory gathers the transactions behind one thing worth remembering: the
// flights, hotel and meals of a holiday, or the single purchase that a year of
// saving went towards.
//
// It holds transaction IDs rather than copies. Transactions are rewritten on
// every sync, so tagging the transaction itself would be lost the next time the
// bank feed ran, while an ID survives.
type Memory struct {
	Id             string    `json:"id"`
	Title          string    `json:"title"`
	Kind           string    `json:"kind"`
	Note           string    `json:"note"`
	TransactionIDs []string  `json:"transaction_ids"`
	CreatedAt      time.Time `json:"created_at"`
}

func (m Memory) Key() []byte    { return []byte(m.Id) }
func (m Memory) Bucket() []byte { return buckets.MemoryBucket }

func (m *Memory) Value() ([]byte, error) { return json.Marshal(m) }

// TimelinePreferences records what the owner has told the timeline to leave
// out. Every list is kept rather than deleting anything, so a hidden purchase
// or a dismissed suggestion can always be brought back.
type TimelinePreferences struct {
	HiddenTransactions   []string `json:"hidden_transactions"`
	DismissedSuggestions []string `json:"dismissed_suggestions"`

	// HiddenMerchants leaves out everything from a merchant, for payments that
	// recur often enough to be tedious to hide one at a time but irregularly
	// enough to look notable: investment top ups, council rates.
	HiddenMerchants []string `json:"hidden_merchants"`
}

func (tp TimelinePreferences) Key() []byte    { return []byte("timeline") }
func (tp TimelinePreferences) Bucket() []byte { return buckets.SettingsBucket }
