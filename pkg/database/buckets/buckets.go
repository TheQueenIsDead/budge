package buckets

var (
	AccountBucket     = []byte("accounts")
	MerchantBucket    = []byte("merchants")
	TransactionBucket = []byte("transactions")
)

func All() [][]byte {
	return [][]byte{
		AccountBucket,
		MerchantBucket,
		TransactionBucket,
	}
}
