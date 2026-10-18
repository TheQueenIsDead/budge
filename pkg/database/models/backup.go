package models

import "time"

// BackupVersion is the shape of the backup file this build writes. An import
// refuses a newer one rather than half-reading fields it does not know about.
const BackupVersion = 1

// Backup is everything set up by hand, for moving to a new instance. Synced
// data is left out: accounts and transactions come back from Akahu on the
// first sync, and because memories and hidden purchases hold Akahu's own
// transaction IDs, they reattach to them when it does. Akahu tokens are left
// out too, so a backup is safe to keep anywhere: they are entered again on the
// new instance.
type Backup struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`

	Memories []Memory            `json:"memories"`
	Timeline TimelinePreferences `json:"timeline"`

	Assets []Asset `json:"assets"`

	BudgetItems  []BudgetItem  `json:"budget_items"`
	BudgetSalary *BudgetSalary `json:"budget_salary,omitempty"`

	Schedule *ScheduleSettings `json:"schedule,omitempty"`

	// RemovedAccounts are the accounts switched off, so the first sync on a
	// new instance does not bring them back.
	RemovedAccounts []RemovedAccount `json:"removed_accounts,omitempty"`

	// SpendRules are the keywords and merchants the owner gave the insights.
	SpendRules *SpendRules `json:"spend_rules,omitempty"`

	// InsightExclusions are transactions left out of the insights, by Akahu's
	// own IDs, so they reattach when the transactions sync back.
	InsightExclusions []string `json:"insight_exclusions,omitempty"`
}

// BackupCounts says what a backup holds, for telling the owner what an import
// brought in.
type BackupCounts struct {
	Memories    int
	Assets      int
	BudgetItems int
}

func (b Backup) Counts() BackupCounts {
	return BackupCounts{Memories: len(b.Memories), Assets: len(b.Assets), BudgetItems: len(b.BudgetItems)}
}
