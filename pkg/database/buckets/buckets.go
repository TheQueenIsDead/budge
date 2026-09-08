package buckets

var (
	AccountBucket      = []byte("accounts")
	AssetBucket        = []byte("assets")
	InventoryBucket    = []byte("inventory")
	MerchantBucket     = []byte("merchants")
	NotificationBucket = []byte("notifications")
	TransactionBucket  = []byte("transactions")
	SettingsBucket     = []byte("settings")
	BudgetBucket       = []byte("budget")
)

// All returns the name of all buckets. This is used for the initial creation of collections in bbolt db.
func All() [][]byte {
	return [][]byte{
		AccountBucket,
		AssetBucket,
		InventoryBucket,
		MerchantBucket,
		NotificationBucket,
		TransactionBucket,
		SettingsBucket,
		BudgetBucket,
	}
}
