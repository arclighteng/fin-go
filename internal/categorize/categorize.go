// Package categorize provides rule-based transaction categorization.
//
// Rules are compiled once at package initialization. CategorizeMerchant is safe
// for concurrent use from multiple goroutines.
package categorize

import (
	"regexp"
	"strings"
)

// Category describes a standard transaction category.
type Category struct {
	ID    string // Short identifier, e.g. "food", "utilities"
	Name  string // Display name, e.g. "Food & Dining"
	Icon  string // Emoji for display
	Color string // CSS color variable
}

// Categories is the complete standard category set, keyed by ID.
var Categories = map[string]Category{
	"income":           {"income", "Income", "\U0001f4b0", "var(--accent-green)"},
	"one_time_deposit": {"one_time_deposit", "One-Time Deposits", "\U0001f4b5", "var(--accent-green)"},
	"housing":          {"housing", "Housing & Rent", "\U0001f3e0", "var(--accent-blue)"},
	"debt_payment":     {"debt_payment", "Debt Payments", "\U0001f3e6", "var(--accent-red)"},
	"utilities":        {"utilities", "Utilities", "\u26a1", "var(--accent-yellow)"},
	"groceries":        {"groceries", "Groceries", "\U0001f6d2", "var(--accent-green)"},
	"dining":           {"dining", "Dining & Restaurants", "\U0001f37d\ufe0f", "var(--accent-yellow)"},
	"transport":        {"transport", "Transportation", "\U0001f697", "var(--accent-blue)"},
	"entertainment":    {"entertainment", "Entertainment", "\U0001f3ac", "var(--accent-purple)"},
	"shopping":         {"shopping", "Shopping", "\U0001f6cd\ufe0f", "var(--accent-purple)"},
	"health":           {"health", "Health & Medical", "\U0001f3e5", "var(--accent-red)"},
	"insurance":        {"insurance", "Insurance", "\U0001f6e1\ufe0f", "var(--accent-blue)"},
	"subscriptions":    {"subscriptions", "Subscriptions", "\U0001f4f1", "var(--accent-purple)"},
	"travel":           {"travel", "Travel", "\u2708\ufe0f", "var(--accent-blue)"},
	"education":        {"education", "Education", "\U0001f4da", "var(--accent-blue)"},
	"personal":         {"personal", "Personal Care", "\U0001f487", "var(--accent-purple)"},
	"pets":             {"pets", "Pets & Pet Care", "\U0001f43e", "var(--accent-yellow)"},
	"gifts":            {"gifts", "Gifts & Donations", "\U0001f381", "var(--accent-green)"},
	"fees":             {"fees", "Fees & Charges", "\U0001f4b3", "var(--accent-red)"},
	"transfer":         {"transfer", "Transfers", "\u2194\ufe0f", "var(--text-muted)"},
	"other":            {"other", "Other", "\U0001f4e6", "var(--text-secondary)"},
}

// Rule holds a compiled regex, the target category ID, and the confidence score.
type Rule struct {
	Pattern    *regexp.Regexp
	CategoryID string
	Confidence float64
}

// rules is the package-level compiled rule set. Populated once by init.
var rules []Rule

func init() {
	type raw struct {
		pattern string
		cat     string
		conf    float64
	}

	rawRules := []raw{
		// Income patterns (recurring wages/salary)
		{`(?i)(payroll|direct deposit|paycheck|salary|unemployment)`, "income", 0.95},

		// One-time deposits (refunds, reimbursements, selling items, etc.)
		{`(?i)(refund|rebate|cashback|cash back|reimbursement|reimburse)`, "one_time_deposit", 0.9},
		{`(?i)(sold|sale proceeds|ebay|poshmark|mercari|offerup|craigslist)`, "one_time_deposit", 0.85},
		{`(?i)(insurance claim|insurance payout|settlement)`, "one_time_deposit", 0.9},
		{`(?i)(gift|inheritance|bonus|award|prize|lottery|winnings)`, "one_time_deposit", 0.85},
		{`(?i)(tax refund|irs treas|state tax)`, "one_time_deposit", 0.9},
		{`(?i)(returned|credit|adjustment|reversal)`, "one_time_deposit", 0.7},

		// Housing (rent only — mortgage is debt)
		{`(?i)(rent|lease|apartment|property|hoa|homeowner)`, "housing", 0.9},

		// Debt Payments — must come before other categories to catch CC payments, loans, mortgages
		{`(?i)(mortgage|home loan|mtg pmt)`, "debt_payment", 0.95},
		{`(?i)(loan|lending|student loan|auto loan|car loan|personal loan)`, "debt_payment", 0.95},
		{`(?i)(credit card|card payment|card pmt|payment.*thank)`, "debt_payment", 0.95},
		// Note: "autopay" omitted — too generic, used by insurance companies too
		{`(?i)(chase card|citi card|discover card|capital one card|amex|american express)`, "debt_payment", 0.95},
		{`(?i)(bank of america|wells fargo|synchrony|barclays).*(payment|pmt)`, "debt_payment", 0.9},
		{`(?i)(payment to|pmt to|pay to).*bank`, "debt_payment", 0.85},

		// Utilities — "gas" changed to "natural gas|gas bill|gas co" to avoid matching gas stations
		{`(?i)(electric|power|natural gas|gas bill|gas co\b|one gas|water|sewage|trash|waste|utility|utilities)`, "utilities", 0.9},
		{`(?i)(comcast|xfinity|at&t|verizon|t-mobile|spectrum|cox|centurylink|google fiber)`, "utilities", 0.9},
		{`(?i)(city of \w+)`, "utilities", 0.85}, // Municipal utilities (simplified for Go regexp)
		{`(?i)(internet|cable|phone|wireless|mobile|telecom)`, "utilities", 0.8},

		// Groceries
		{`(?i)(grocery|groceries|supermarket|whole foods|trader joe|kroger|safeway|publix|aldi|costco|sam's club|walmart supercenter|target)`, "groceries", 0.9},
		{`(?i)(h-e-b|heb|wegmans|food lion|giant|stop.?shop|sprouts|meijer)`, "groceries", 0.9},

		// Dining
		{`(?i)(restaurant|cafe|coffee|starbucks|dunkin|mcdonald|burger|pizza|taco|chipotle|subway|panera|chick-fil-a|wendy)`, "dining", 0.9},
		// Food delivery — higher confidence to beat the transport rule for "uber eats"
		{`(?i)(doordash|uber eats|ubereats|grubhub|postmates|seamless|caviar)`, "dining", 0.95},
		{`(?i)(bar|pub|brewery|tavern|grill)`, "dining", 0.8},

		// Transportation (ride-share, transit, gas stations)
		{`(?i)(uber|lyft|taxi|cab|transit|metro|bus|train|amtrak|parking)`, "transport", 0.9},
		{`(?i)(gas station|shell|chevron|exxon|mobil|bp\b|arco|76 gas|76 station|speedway|wawa)`, "transport", 0.9},
		{`(?i)(car wash|auto|tire|oil change|mechanic|jiffy lube)`, "transport", 0.85},
		{`(?i)(toll|dmv|registration)`, "transport", 0.8},

		// Entertainment
		{`(?i)(netflix|hulu|disney|hbo|paramount|peacock|apple tv|amazon prime video|spotify|pandora|youtube|twitch)`, "entertainment", 0.95},
		{`(?i)(movie|cinema|theater|theatre|amc|regal)`, "entertainment", 0.9},
		{`(?i)(game|playstation|xbox|steam|nintendo|epic games)`, "entertainment", 0.9},
		{`(?i)(concert|ticket|ticketmaster|stubhub|eventbrite|live nation)`, "entertainment", 0.85},

		// Shopping
		{`(?i)(amazon|walmart|target|best buy|home depot|lowe's|ikea|wayfair)`, "shopping", 0.8},
		{`(?i)(ebay|etsy|wish|aliexpress|shein)`, "shopping", 0.85},
		{`(?i)(clothing|apparel|shoes|fashion|zara|h&m|gap|old navy|nike|adidas)`, "shopping", 0.85},

		// Health
		{`(?i)(pharmacy|cvs|walgreens|rite aid|drug|medication|rx)`, "health", 0.9},
		{`(?i)(doctor|medical|hospital|clinic|urgent care|dental|dentist|vision|optom)`, "health", 0.9},
		{`(?i)(carenow|carespot|minute clinic|nextcare|patient first|medexpress)`, "health", 0.95},
		{`(?i)(health|healthcare|fitness|gym|planet fitness|la fitness|equinox|peloton)`, "health", 0.85},

		// Insurance
		{`(?i)(insurance|geico|state farm|allstate|progressive|liberty mutual|usaa)`, "insurance", 0.95},
		{`(?i)(insurance premium|coverage|policy)`, "insurance", 0.7},

		// Subscriptions (general software/services)
		{`(?i)(adobe|microsoft|office|dropbox|google one|icloud|onedrive)`, "subscriptions", 0.9},
		{`(?i)(patreon|substack|medium|linkedin premium|github)`, "subscriptions", 0.85},
		{`(?i)(membership|subscription|recurring|monthly fee)`, "subscriptions", 0.7},

		// Travel
		{`(?i)(airline|united|delta|american|southwest|jetblue|spirit|frontier)`, "travel", 0.95},
		{`(?i)(hotel|marriott|hilton|hyatt|airbnb|vrbo|motel|inn)`, "travel", 0.9},
		{`(?i)(expedia|booking|kayak|priceline|trivago)`, "travel", 0.9},

		// Education
		{`(?i)(university|college|tuition|school|education|course|udemy|coursera|skillshare)`, "education", 0.9},
		{`(?i)(textbook|student loan|sallie mae|navient)`, "education", 0.85},

		// Personal care
		{`(?i)(salon|spa|barber|haircut|nail|massage|wax)`, "personal", 0.9},
		{`(?i)(beauty|cosmetic|sephora|ulta)`, "personal", 0.85},

		// Pets & Pet Care
		{`(?i)(petco|petsmart|pet supplies plus|chewy|pet food|pet store)`, "pets", 0.95},
		{`(?i)(veterinar|vet clinic|animal hospital|animal clinic|banfield|vca\b)`, "pets", 0.95},
		{`(?i)(dog food|cat food|pet meds|pet pharmacy|1800petmeds)`, "pets", 0.9},
		{`(?i)(groomer|pet grooming|dog wash|doggy daycare|pet boarding|pet hotel)`, "pets", 0.9},
		{`(?i)(rover|wag\b|pet sit|dog walk|bark box|barkbox)`, "pets", 0.85},

		// Gifts & donations
		{`(?i)(charity|donation|donate|nonprofit|foundation|red cross|unicef)`, "gifts", 0.9},
		{`(?i)(gift|present|flowers|1-800)`, "gifts", 0.7},

		// Fees
		{`(?i)(fee|charge|interest|overdraft|late fee|penalty|finance charge)`, "fees", 0.85},
		{`(?i)(atm|withdrawal|service charge)`, "fees", 0.8},

		// Transfers (should be excluded from expenses)
		{`(?i)(transfer|zelle|venmo|paypal|cash app|wire|ach)`, "transfer", 0.85},
		{`(?i)(credit card payment|payment to|bill pay)`, "transfer", 0.9},
	}

	rules = make([]Rule, 0, len(rawRules))
	for _, r := range rawRules {
		rules = append(rules, Rule{
			Pattern:    regexp.MustCompile(r.pattern),
			CategoryID: r.cat,
			Confidence: r.conf,
		})
	}
}

// CategorizeMerchant returns the best-matching (categoryID, confidence) pair for
// the given merchant name and raw description. When no rule matches, it returns
// ("other", 0.0).
//
// The function is safe for concurrent use.
func CategorizeMerchant(merchantNorm, description string) (string, float64) {
	text := strings.ToLower(merchantNorm + " " + description)

	bestCat := "other"
	bestConf := 0.0

	for _, r := range rules {
		if r.Pattern.MatchString(text) && r.Confidence > bestConf {
			bestCat = r.CategoryID
			bestConf = r.Confidence
		}
	}

	return bestCat, bestConf
}
