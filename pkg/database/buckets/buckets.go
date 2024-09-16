package buckets

var (
	AccountBucket     = []byte("accounts")
	MerchantBucket    = []byte("merchants")
	TransactionBucket = []byte("transactions")
	SettingsBucket    = []byte("settings")
)

// All returns the name of all buckets. This is used for the initial creation of collections in bbolt db.
func All() [][]byte {
	return [][]byte{
		AccountBucket,
		MerchantBucket,
		TransactionBucket,
		SettingsBucket,
	}
}
