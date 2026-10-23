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

	Categories *CategorySettings `json:"categories,omitempty"`
}

// BackupCounts says what a backup holds, for telling the owner what an import
// brought in.
type BackupCounts struct {
	Memories    int
	Assets      int
	BudgetItems int
	Rules       int
}

func (b Backup) Counts() BackupCounts {
	counts := BackupCounts{Memories: len(b.Memories), Assets: len(b.Assets), BudgetItems: len(b.BudgetItems)}
	if b.Categories != nil {
		counts.Rules = len(b.Categories.Rules)
	}
	return counts
}
