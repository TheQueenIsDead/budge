package models

import (
	"strconv"
	"strings"
	"sync"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// Budge keeps its own short list of categories, the owner's to rename,
// add to and delete, and every transaction is filed under one of them. Akahu's categories are an input to that, not
// something shown: each of Akahu's fifty-odd maps to one of these, and rules
// and the owner's own corrections can file anything anywhere.
//
// There are two levels. Categories are what a budget target is set against.
// Subcategories are optional, and only exist where the finer split is worth
// watching on its own, such as power within bills or fuel within transport.

// Each category is on budget, taking a target and counting as spending, or
// off it: money that leaves without being spent, such as savings. The one
// exception is Uncategorised, which takes no target but is spending all the
// same, only not filed yet.

// BudgetCategory is a category or, when it has a Parent, a subcategory.
type BudgetCategory struct {
	Key    string
	Label  string
	Parent string
	Icon   string
	Accent string
	Blurb  string

	// OffBudget marks a category that takes no target and is not spending.
	// A subcategory follows its category.
	OffBudget bool

	// Bill marks a category paid as a bill, arriving once a period rather
	// than bought as needed: housing, utilities and insurance. It is not
	// shown, but decides how the category is charted and that a bill is not
	// a purchase worth remembering. A subcategory follows its category.
	Bill bool

	// Group is where a category from before budge had on and off budget sat;
	// it is read only to carry such a list over.
	Group string `json:",omitempty"`

	// NotSpending marks money that leaves without being spent: savings, and
	// loan interest the repayments under Home already pay. It is everything
	// off budget but Uncategorised.
	NotSpending bool `json:"-"`

	// Frequency is what a target for the category is set per until one is
	// saved: weekly unless the category is too lumpy to judge week by week.
	Frequency string
}

// Category keys the code refers to by name.
const (
	CategoryUncategorised = "uncategorised"
	CategoryOther         = "other"
)

// Groups a category could sit under before budge had on and off budget,
// read only to carry a saved list over.
const (
	legacyGroupUnbudgeted = "unbudgeted"
	legacyGroupBills      = "bills"
)

// defaultBudgetCategories is the list budge starts with, in display order. A
// subcategory follows its parent and takes whether it is on budget, whether
// it is a bill, and its accent from it, and its icon unless it has its own.
var defaultBudgetCategories = []BudgetCategory{
	{Key: "home", Label: "Home", Bill: true, Icon: "bi-house", Accent: "slate", Blurb: "Mortgage, rent and rates"},
	{Key: "mortgage", Label: "Mortgage", Parent: "home"},
	{Key: "rates", Label: "Rates", Parent: "home", Frequency: "quarterly"},
	{Key: "rent", Label: "Rent", Parent: "home"},

	{Key: "bills", Label: "Bills & subscriptions", Bill: true, Icon: "bi-receipt-cutoff", Accent: "plum", Blurb: "Power, internet, mobile and software"},
	{Key: "power", Label: "Power", Parent: "bills", Icon: "bi-lightning-charge", Blurb: "Electricity and gas", Frequency: "monthly"},
	{Key: "internet-mobile", Label: "Internet & mobile", Parent: "bills", Icon: "bi-wifi", Blurb: "Broadband and phone plans", Frequency: "monthly"},
	{Key: "software", Label: "Software & subscriptions", Parent: "bills"},

	{Key: "insurance", Label: "Insurance", Bill: true, Icon: "bi-shield-check", Accent: "slate", Blurb: "House, contents, vehicle and pet cover"},

	{Key: "groceries", Label: "Groceries", Icon: "bi-basket", Accent: "sage", Blurb: "Supermarkets, dairies and food shops"},

	{Key: "transport", Label: "Transport", Icon: "bi-car-front", Accent: "clay", Blurb: "Fuel, rego, parking and servicing"},
	{Key: "fuel", Label: "Fuel", Parent: "transport", Icon: "bi-fuel-pump", Blurb: "Fuel stations"},
	{Key: "rego", Label: "Rego, tolls & fares", Parent: "transport"},
	{Key: "servicing", Label: "Servicing & parts", Parent: "transport"},
	{Key: "parking", Label: "Parking", Parent: "transport"},

	{Key: "eating-out", Label: "Eating & drinking out", Icon: "bi-cup-hot", Accent: "brick", Blurb: "Cafes, takeaways, restaurants and bars"},

	{Key: "alcohol-tobacco", Label: "Alcohol & tobacco", Icon: "bi-cup-straw", Accent: "muted", Blurb: "Liquor stores, smoking and vape"},
	{Key: "tobacco", Label: "Tobacco & vape", Parent: "alcohol-tobacco"},
	{Key: "liquor", Label: "Liquor", Parent: "alcohol-tobacco"},

	{Key: "pets", Label: "Pets", Icon: "bi-heart", Accent: "sage", Blurb: "Food, supplies and vets"},
	{Key: "pet-care", Label: "Pet supplies & care", Parent: "pets"},
	{Key: "vets", Label: "Vets", Parent: "pets"},

	{Key: "health", Label: "Health", Icon: "bi-heart-pulse", Accent: "brick", Blurb: "Pharmacies, doctors and fitness"},
	{Key: "pharmacy", Label: "Pharmacy", Parent: "health"},
	{Key: "fitness", Label: "Fitness", Parent: "health"},
	{Key: "doctors", Label: "Doctors & dentists", Parent: "health"},

	{Key: "maintenance", Label: "Maintenance", Icon: "bi-tools", Accent: "clay", Blurb: "Repairs, upkeep, hardware and garden"},
	{Key: "hardware", Label: "Hardware & supplies", Parent: "maintenance"},
	{Key: "trades", Label: "Trades & repairs", Parent: "maintenance"},
	{Key: "home-garden", Label: "Home & garden", Parent: "maintenance"},

	{Key: "shopping", Label: "Shopping", Icon: "bi-bag", Accent: "plum", Blurb: "Clothes, electronics and general retail"},
	{Key: "general-shopping", Label: "General & online", Parent: "shopping"},
	{Key: "electronics", Label: "Electronics & games", Parent: "shopping"},
	{Key: "clothing", Label: "Clothing & shoes", Parent: "shopping"},

	{Key: "entertainment", Label: "Entertainment & travel", Icon: "bi-ticket-perforated", Accent: "slate", Blurb: "Events, hotels and flights"},
	{Key: "events", Label: "Events & activities", Parent: "entertainment"},
	{Key: "travel", Label: "Travel", Parent: "entertainment"},

	{Key: "big-purchases", Label: "Big purchases", Icon: "bi-bag-heart", Accent: "clay", Blurb: "Vehicles and anything else large, against a yearly target", Frequency: "yearly"},
	{Key: "vehicles", Label: "Vehicles", Parent: "big-purchases"},

	{Key: CategoryOther, Label: "Other", Icon: "bi-three-dots", Accent: "muted", Blurb: "Cash, charity and anything unplaced"},
	{Key: "cash", Label: "Cash", Parent: CategoryOther},
	{Key: "charity", Label: "Charity", Parent: CategoryOther},

	{Key: "savings", Label: "Savings & investments", OffBudget: true, Icon: "bi-piggy-bank", Accent: "sage", Blurb: "Money put away, not spent"},
	{Key: "loan-interest", Label: "Loan interest", OffBudget: true, Icon: "bi-percent", Accent: "muted", Blurb: "Already paid by the repayments under Home"},
	{Key: CategoryUncategorised, Label: "Uncategorised", OffBudget: true, Icon: "bi-question-circle", Accent: "muted", Blurb: "Spending no category or rule files yet"},
}

// BudgetCategorySettings is the owner's category list, saved once anything
// in it changes; until then budge's own list is used. Merged records where
// the transactions of each deleted category went, so a rule, a correction or
// Akahu's category that named it still lands somewhere.
type BudgetCategorySettings struct {
	Categories []BudgetCategory
	Merged     map[string]string
}

func (bs BudgetCategorySettings) Key() []byte    { return []byte("budget-categories") }
func (bs BudgetCategorySettings) Bucket() []byte { return buckets.SettingsBucket }

// DefaultBudgetCategories is the list budge starts with.
func DefaultBudgetCategories() []BudgetCategory {
	return append([]BudgetCategory(nil), defaultBudgetCategories...)
}

// budgetCategoryRegistry is the category list in use. It is read on every
// transaction, and changed only when the owner edits it, so it is held here
// rather than read from the database each time.
var budgetCategoryRegistry struct {
	sync.RWMutex
	list   []BudgetCategory
	index  map[string]BudgetCategory
	merged map[string]string
}

func init() { UseBudgetCategories(BudgetCategorySettings{}) }

// UseBudgetCategories makes a saved category list the one in use; an empty
// one means budge's own. Subcategories take their parent's place on or off
// budget and their accent here, so they are stored without them.
func UseBudgetCategories(settings BudgetCategorySettings) {
	list := settings.Categories
	if len(list) == 0 {
		list = defaultBudgetCategories
	}
	index := make(map[string]BudgetCategory, len(list))
	for _, category := range list {
		category = carriedOver(category)
		if parent, ok := index[category.Parent]; ok {
			category.OffBudget = parent.OffBudget
			category.Bill = parent.Bill
			category.Accent = parent.Accent
			if category.Icon == "" {
				category.Icon = parent.Icon
			}
			if category.Frequency == "" {
				category.Frequency = parent.Frequency
			}
		}
		if category.Key == CategoryUncategorised {
			category.OffBudget = true
		}
		category.NotSpending = category.OffBudget && category.Key != CategoryUncategorised
		index[category.Key] = category
	}

	budgetCategoryRegistry.Lock()
	defer budgetCategoryRegistry.Unlock()
	budgetCategoryRegistry.list = append([]BudgetCategory(nil), list...)
	budgetCategoryRegistry.index = index
	budgetCategoryRegistry.merged = settings.Merged
}

// carriedOver reads a category saved before budge had on and off budget: its
// group said both whether it was budgeted and whether it was a bill.
func carriedOver(category BudgetCategory) BudgetCategory {
	switch category.Group {
	case legacyGroupUnbudgeted:
		category.OffBudget = true
	case legacyGroupBills:
		category.Bill = true
	}
	category.Group = ""
	return category
}

// resolve follows a key on from any category it was merged into, reporting
// whether it leads to one that exists. The registry must be locked.
func resolve(key string) (string, bool) {
	for range len(budgetCategoryRegistry.merged) + 1 {
		if _, ok := budgetCategoryRegistry.index[key]; ok {
			return key, true
		}
		next, ok := budgetCategoryRegistry.merged[key]
		if !ok {
			break
		}
		key = next
	}
	return "", false
}

// ResolveBudgetCategory follows a key on from any category it was merged
// into, and is Uncategorised when it leads nowhere.
func ResolveBudgetCategory(key string) string {
	budgetCategoryRegistry.RLock()
	defer budgetCategoryRegistry.RUnlock()
	if resolved, ok := resolve(key); ok {
		return resolved
	}
	return CategoryUncategorised
}

// BudgetCategoryByKey looks up a category or subcategory. A deleted one is
// found as the category its transactions went to.
func BudgetCategoryByKey(key string) (BudgetCategory, bool) {
	budgetCategoryRegistry.RLock()
	defer budgetCategoryRegistry.RUnlock()
	resolved, ok := resolve(key)
	if !ok {
		return BudgetCategory{}, false
	}
	return budgetCategoryRegistry.index[resolved], true
}

// TopBudgetCategory is the category a key budgets under: itself, or the
// parent of a subcategory. An unknown key is uncategorised.
func TopBudgetCategory(key string) BudgetCategory {
	key = ResolveBudgetCategory(key)
	budgetCategoryRegistry.RLock()
	defer budgetCategoryRegistry.RUnlock()
	category := budgetCategoryRegistry.index[key]
	if category.Parent != "" {
		return budgetCategoryRegistry.index[category.Parent]
	}
	return category
}

// IsSub reports whether this is a subcategory.
func (c BudgetCategory) IsSub() bool { return c.Parent != "" }

// Budgeted reports whether a target can be set against the category.
func (c BudgetCategory) Budgeted() bool { return !c.OffBudget }

// TopBudgetCategories lists the categories in display order, without their
// subcategories.
func TopBudgetCategories() []BudgetCategory {
	budgetCategoryRegistry.RLock()
	defer budgetCategoryRegistry.RUnlock()
	var top []BudgetCategory
	for _, category := range budgetCategoryRegistry.list {
		if category.Parent == "" {
			top = append(top, budgetCategoryRegistry.index[category.Key])
		}
	}
	return top
}

// BudgetSubcategories lists a category's subcategories in display order.
func BudgetSubcategories(parent string) []BudgetCategory {
	budgetCategoryRegistry.RLock()
	defer budgetCategoryRegistry.RUnlock()
	var subs []BudgetCategory
	for _, category := range budgetCategoryRegistry.list {
		if category.Parent == parent {
			subs = append(subs, budgetCategoryRegistry.index[category.Key])
		}
	}
	return subs
}

// akahuBySpecific places Akahu's specific categories that do not simply go
// where their broad category does. Keys are lower cased. The owner's own
// categories from before budge had its own, which rules may still name, are
// here too.
var akahuBySpecific = map[string]string{
	"local government": "rates",

	"electricity services":                 "power",
	"gas services":                         "power",
	"internet services":                    "internet-mobile",
	"telecommunication services":           "internet-mobile",
	"business software and cloud services": "software",

	"insurance": "insurance",

	"supermarkets and grocery stores": "groceries",
	"convenience stores":              "groceries",
	"specialty food stores":           "groceries",
	"bakeries":                        "groceries",
	"meat supplies":                   "groceries",
	"ice cream, gelato, nut, and confectionary stores": "eating-out",

	"fuel stations": "fuel",
	"transport services (not elsewhere classified)": "rego",
	"automotive repair and servicing":               "servicing",
	"automotive parts and accessories":              "servicing",
	"parking services":                              "parking",
	"car, truck, and motorcycle dealers":            "vehicles",
	"air transport services":                        "travel",

	"cafes and restaurants":                                           "eating-out",
	"fast food stores":                                                "eating-out",
	"bars, pubs, nightclubs":                                          "eating-out",
	"caterers":                                                        "eating-out",
	"wineries, breweries, and distilleries":                           "eating-out",
	"cigarette, vape, and other smoking products":                     "tobacco",
	"liquor stores":                                                   "liquor",
	"events and tickets (not elsewhere classified)":                   "events",
	"entertainment (not elsewhere classified)":                        "events",
	"theatre, concerts, and other artistic performances":              "events",
	"attractions, museums, zoos, amusement parks, circuses, exhibits": "events",
	"event venue and equipment rental":                                "events",
	"hotels, motels, and other temporary accommodation":               "travel",
	"digital gaming products and services":                            "electronics",
	"musical equipment, recordings, and supplies":                     "electronics",
	"welfare and charity":                                             "charity",

	"pets and related supplies, accommodation, and services": "pet-care",
	"veterinary services": "vets",
	"pharmacies":          "pharmacy",
	"gyms, fitness, aquatic facilities, yoga, pilates": "fitness",

	"building supplies": "hardware",
	"industrial and commercial supplies (not elsewhere classified)": "hardware",
	"appliance repair and services":                                 "trades",
	"plumbing and gasfitting supplies and services":                 "trades",
	"home furnishing and repair stores":                             "home-garden",
	"nurseries and garden supplies":                                 "home-garden",
	"laundry and drycleaning":                                       "home-garden",

	"general retail stores":                          "general-shopping",
	"variety stores":                                 "general-shopping",
	"stationery and office supplies":                 "general-shopping",
	"department stores":                              "general-shopping",
	"electronic and appliance stores":                "electronics",
	"clothing stores":                                "clothing",
	"shoe stores":                                    "clothing",
	"fabric, sewing, knitting, and related supplies": "clothing",

	// Categories the owner made before budge had its own.
	"mortgage": "mortgage",
	"savings":  "savings",
}

// akahuByBroad is where each of Akahu's broad categories goes when its
// specific category is not placed above. Keys are lower cased.
var akahuByBroad = map[string]string{
	"housing":    "home",
	"utilities":  "bills",
	"food":       "groceries",
	"transport":  "transport",
	"lifestyle":  "entertainment",
	"household":  "maintenance",
	"health":     "health",
	"appearance": "shopping",
}

// akahuByType places debits Akahu gave no category, by transaction type.
var akahuByType = map[string]string{
	"LOAN": "loan-interest",
	"ATM":  "cash",
}

// AkahuBudgetCategory places a transaction by the category Akahu, or a
// category from before budge had its own, gave it. With no category at all,
// its type decides.
func AkahuBudgetCategory(broad, specific, txType string) string {
	if key, ok := akahuBySpecific[strings.ToLower(specific)]; ok {
		return key
	}
	if broad == "" && specific == "" {
		if key, ok := akahuByType[txType]; ok {
			return key
		}
		return CategoryUncategorised
	}
	if key, ok := akahuByBroad[strings.ToLower(broad)]; ok {
		return key
	}
	return CategoryOther
}

// BudgetCategory is where the transaction is filed: by the owner, a rule or
// Akahu, in that order. Reading through the store files it; a transaction
// made any other way falls back to Akahu's category.
func (t Transaction) BudgetCategory() string {
	if t.Filed != "" {
		return ResolveBudgetCategory(t.Filed)
	}
	return ResolveBudgetCategory(AkahuBudgetCategory(t.Category.Groups.PersonalFinance.Name, t.Category.Name, t.Type))
}

// Editing the category list. Each edit works on a copy, starting from budge's
// own list the first time, and is saved by the caller.

// BudgetCategoryIcons are the icons a category can wear.
var BudgetCategoryIcons = []string{
	"bi-house", "bi-receipt-cutoff", "bi-shield-check", "bi-basket", "bi-car-front", "bi-cup-hot",
	"bi-cup-straw", "bi-heart", "bi-heart-pulse", "bi-tools", "bi-bag", "bi-ticket-perforated",
	"bi-bag-heart", "bi-three-dots", "bi-piggy-bank", "bi-percent", "bi-question-circle",
	"bi-lightning-charge", "bi-wifi", "bi-fuel-pump", "bi-airplane", "bi-mortarboard", "bi-gift",
	"bi-controller", "bi-music-note-beamed", "bi-book", "bi-scissors", "bi-tree", "bi-bicycle",
	"bi-bus-front", "bi-capsule", "bi-briefcase", "bi-people", "bi-balloon", "bi-film", "bi-phone",
}

// BudgetCategoryAccents are the colours a category can wear.
var BudgetCategoryAccents = []string{"sage", "clay", "plum", "slate", "brick", "muted"}

// BudgetCategoryError is an edit refused, worded for the owner.
type BudgetCategoryError string

func (e BudgetCategoryError) Error() string { return string(e) }

// editable is the list to change: the saved one, or budge's own the first
// time, copied so a refused edit changes nothing.
func (bs BudgetCategorySettings) editable() BudgetCategorySettings {
	list := bs.Categories
	if len(list) == 0 {
		list = defaultBudgetCategories
	}
	list = append([]BudgetCategory(nil), list...)
	for i := range list {
		list[i] = carriedOver(list[i])
	}
	merged := make(map[string]string, len(bs.Merged))
	for from, to := range bs.Merged {
		merged[from] = to
	}
	return BudgetCategorySettings{Categories: append([]BudgetCategory(nil), list...), Merged: merged}
}

func (bs BudgetCategorySettings) find(key string) int {
	for i, category := range bs.Categories {
		if category.Key == key {
			return i
		}
	}
	return -1
}

// block is where a category and its subcategories sit in the list.
func (bs BudgetCategorySettings) block(i int) (start, end int) {
	end = i + 1
	for end < len(bs.Categories) && bs.Categories[end].Parent == bs.Categories[i].Key {
		end++
	}
	return i, end
}

// newKey makes a key for a new category from its name. A key is never
// reused, even one deleted, so nothing that named the old one finds the new.
func (bs BudgetCategorySettings) newKey(label string) string {
	var slug strings.Builder
	dash := false
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slug.WriteRune(r)
			dash = false
		} else if !dash && slug.Len() > 0 {
			slug.WriteRune('-')
			dash = true
		}
	}
	base := strings.TrimSuffix(slug.String(), "-")
	if base == "" {
		base = "category"
	}
	taken := func(key string) bool {
		_, merged := bs.Merged[key]
		_, builtin := defaultIndex[key]
		return bs.find(key) >= 0 || merged || builtin
	}
	key := base
	for n := 2; taken(key); n++ {
		key = base + "-" + strconv.Itoa(n)
	}
	return key
}

// defaultIndex holds budge's own keys, which are kept for the categories
// they started as even once deleted.
var defaultIndex = func() map[string]bool {
	index := make(map[string]bool, len(defaultBudgetCategories))
	for _, category := range defaultBudgetCategories {
		index[category.Key] = true
	}
	return index
}()

// AddBudgetCategory adds a category, off budget until it is put on, before
// Uncategorised; or, given a parent, a subcategory at the end of its
// parent's. It returns the new key.
func (bs BudgetCategorySettings) AddBudgetCategory(label, parent string) (BudgetCategorySettings, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return bs, "", BudgetCategoryError("A category needs a name.")
	}
	next := bs.editable()
	category := BudgetCategory{Key: next.newKey(label), Label: label}

	at := len(next.Categories)
	if parent != "" {
		p := next.find(parent)
		if p < 0 || next.Categories[p].Parent != "" {
			return bs, "", BudgetCategoryError("That category no longer exists.")
		}
		category.Parent = parent
		_, at = next.block(p)
	} else {
		category.OffBudget = true
		category.Icon = "bi-three-dots"
		category.Accent = "muted"
		if u := next.find(CategoryUncategorised); u >= 0 {
			at = u
		}
	}
	next.Categories = append(next.Categories[:at], append([]BudgetCategory{category}, next.Categories[at:]...)...)
	return next, category.Key, nil
}

// BudgetCategoryChange is an edit to a category's name and look, and, when
// OffBudget is given, whether it is on budget. Icon, accent and budget are
// ignored for a subcategory, which takes its parent's.
type BudgetCategoryChange struct {
	Label     string
	Blurb     string
	Icon      string
	Accent    string
	OffBudget *bool
}

// UpdateBudgetCategory renames a category or changes how it looks. Its key
// stays, so the rules, corrections and target that name it stay with it.
func (bs BudgetCategorySettings) UpdateBudgetCategory(key string, change BudgetCategoryChange) (BudgetCategorySettings, error) {
	change.Label = strings.TrimSpace(change.Label)
	if change.Label == "" {
		return bs, BudgetCategoryError("A category needs a name.")
	}
	next := bs.editable()
	i := next.find(key)
	if i < 0 {
		return bs, BudgetCategoryError("That category no longer exists.")
	}
	category := &next.Categories[i]
	category.Label = change.Label
	category.Blurb = strings.TrimSpace(change.Blurb)
	if category.Parent != "" {
		return next, nil
	}
	if change.Icon != "" {
		category.Icon = change.Icon
	}
	if change.Accent != "" {
		category.Accent = change.Accent
	}
	if change.OffBudget == nil || *change.OffBudget == category.OffBudget {
		return next, nil
	}
	if key == CategoryUncategorised {
		return bs, BudgetCategoryError("Uncategorised is never budgeted, since nothing is filed there on purpose.")
	}
	category.OffBudget = *change.OffBudget
	return next, nil
}

// DeleteBudgetCategory removes a category, and its subcategories, sending
// everything filed under them on to another. Rules, corrections and Akahu's
// categories that named them follow, so nothing is left behind.
func (bs BudgetCategorySettings) DeleteBudgetCategory(key, into string) (BudgetCategorySettings, error) {
	if key == CategoryUncategorised {
		return bs, BudgetCategoryError("Uncategorised is where anything unfiled goes, so it stays.")
	}
	next := bs.editable()
	i := next.find(key)
	if i < 0 {
		return bs, BudgetCategoryError("That category no longer exists.")
	}
	if into == "" && next.Categories[i].Parent != "" {
		into = next.Categories[i].Parent
	}
	start, end := next.block(i)
	j := next.find(into)
	if j < 0 || (j >= start && j < end) {
		return bs, BudgetCategoryError("Pick where its transactions should go.")
	}
	for _, gone := range next.Categories[start:end] {
		next.Merged[gone.Key] = into
	}
	next.Categories = append(next.Categories[:start], next.Categories[end:]...)
	return next, nil
}
