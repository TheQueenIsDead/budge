package buckets

var (
	AccountBucket     = []byte("accounts")
	InventoryBucket   = []byte("inventory")
	MerchantBucket    = []byte("merchants")
	TransactionBucket = []byte("transactions")
)

// All returns the name of all buckets. This is used for the initial creation of collections in bbolt db.
func All() [][]byte {
	return [][]byte{
		AccountBucket,
		InventoryBucket,
		MerchantBucket,
		TransactionBucket,
	}
}
