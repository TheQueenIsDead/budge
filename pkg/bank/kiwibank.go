package bank

import (
	"fmt"
	"github.com/scylladb/go-set/strset"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"regexp"
	"strings"
)

type KiwibankExportRow struct {
	AccountNumber         string
	Date                  string
	MemoDescription       string
	SourceCodePaymentType string
	TPref                 string
	TPpart                string
	TPcode                string
	OPref                 string
	OPpart                string
	OPcode                string
	OPname                string
	OPBankAccountNumber   string
	AmountCredit          string
	AmountDebit           string
	Amount                string
	Balance               string
}

func (k *KiwibankExportRow) MerchantName() string {
	if k.OPname != "" && k.OPBankAccountNumber != "" {
		return k.OPname
	}

	// Try tidy up the memo into a passable name
	name := ""
	parts := strings.Split(k.MemoDescription, " ")
	dropParts := strset.New("POS", "W/D", ";")
	for _, part := range parts {
		if dropParts.Has(part) {
			continue
		}
		caser := cases.Title(language.English)
		name = fmt.Sprintf("%s %s", name, caser.String(part))
	}

	re := regexp.MustCompile(`-[0-9]{2}:[0-9]{2}`)
	name = re.ReplaceAllString(name, "")

	name = strings.Replace(name, ";", "", -1)

	if name != "" {
		return name
	}

	return k.MemoDescription
}
