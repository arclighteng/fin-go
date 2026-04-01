package categorize

import (
	"testing"
)

// TestCategorizeMerchantKnownMerchants verifies that well-known merchant names
// resolve to the expected category.
func TestCategorizeMerchantKnownMerchants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		merchant    string
		description string
		wantCat     string
	}{
		// Dining
		{"Starbucks", "starbucks", "", "dining"},
		{"McDonald's", "mcdonald", "", "dining"},
		{"DoorDash", "doordash", "", "dining"},
		{"Uber Eats via description", "", "uber eats order", "dining"},
		{"Chipotle", "chipotle", "", "dining"},
		{"Grubhub", "grubhub", "", "dining"},

		// Groceries
		{"Whole Foods", "whole foods", "", "groceries"},
		{"Trader Joe's", "trader joe", "", "groceries"},
		{"Kroger", "kroger", "", "groceries"},
		{"Walmart Supercenter", "walmart supercenter", "", "groceries"},
		{"Costco", "costco", "", "groceries"},
		{"HEB", "heb", "", "groceries"},

		// Transportation
		{"Shell Gas Station", "shell", "", "transport"},
		{"Chevron", "chevron", "", "transport"},
		{"Lyft", "lyft", "", "transport"},
		{"Uber (ride, not eats)", "uber", "", "transport"},
		{"Parking", "downtown parking", "", "transport"},
		{"Exxon", "exxon", "", "transport"},

		// Entertainment
		{"Netflix", "netflix", "", "entertainment"},
		{"Spotify", "spotify", "", "entertainment"},
		{"AMC Theaters", "amc theater", "", "entertainment"},
		{"Steam", "steam games", "", "entertainment"},
		{"Disney Plus", "disney", "", "entertainment"},

		// Utilities
		{"Comcast", "comcast", "", "utilities"},
		{"AT&T", "at&t", "", "utilities"},
		{"Verizon", "verizon", "", "utilities"},
		{"Electric company", "electric bill", "", "utilities"},

		// Subscriptions
		{"Adobe", "adobe", "", "subscriptions"},
		{"Microsoft", "microsoft", "", "subscriptions"},
		{"Dropbox", "dropbox", "", "subscriptions"},
		{"Patreon", "patreon", "", "subscriptions"},

		// Health
		{"CVS Pharmacy", "cvs", "", "health"},
		{"Walgreens", "walgreens", "", "health"},
		{"Urgent care", "urgent care", "", "health"},
		{"Dentist", "dentist office", "", "health"},
		{"Planet Fitness", "planet fitness", "", "health"},

		// Insurance
		{"Geico", "geico", "", "insurance"},
		{"State Farm", "state farm", "", "insurance"},
		{"Progressive", "progressive insurance", "", "insurance"},

		// Pets
		{"Petco", "petco", "", "pets"},
		{"PetSmart", "petsmart", "", "pets"},
		{"Chewy", "chewy", "", "pets"},
		{"Vet clinic", "veterinary clinic", "", "pets"},

		// Housing
		{"Rent payment", "rent", "", "housing"},
		{"HOA dues", "hoa", "", "housing"},
		{"Apartment", "apartment payment", "", "housing"},

		// Debt payments
		{"Mortgage", "mortgage payment", "", "debt_payment"},
		{"Chase credit card", "chase card", "", "debt_payment"},
		{"Student loan", "student loan", "", "debt_payment"},

		// Travel
		{"United Airlines", "united airlines", "", "travel"},
		{"Marriott Hotel", "marriott hotel", "", "travel"},
		{"Airbnb", "airbnb", "", "travel"},
		{"Expedia", "expedia", "", "travel"},

		// Education
		{"Udemy", "udemy", "", "education"},
		{"Coursera", "coursera", "", "education"},

		// Gifts & donations
		{"Red Cross", "red cross", "", "gifts"},

		// Fees
		{"Overdraft fee", "overdraft fee", "", "fees"},
		{"ATM withdrawal", "atm withdrawal", "", "fees"},

		// Transfers
		{"Venmo", "venmo", "", "transfer"},
		{"Zelle", "zelle", "", "transfer"},
		{"PayPal", "paypal", "", "transfer"},

		// Income
		{"Payroll deposit", "payroll direct deposit", "", "income"},
		{"Direct deposit", "direct deposit", "", "income"},

		// Shopping
		{"Amazon", "amazon.com", "", "shopping"},
		{"Best Buy", "best buy", "", "shopping"},

		// Personal care
		{"Salon", "hair salon", "", "personal"},
		{"Spa", "day spa", "", "personal"},
		{"Sephora", "sephora", "", "personal"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotCat, gotConf := CategorizeMerchant(tc.merchant, tc.description)
			if gotCat != tc.wantCat {
				t.Errorf("CategorizeMerchant(%q, %q): want %q, got %q (conf=%.2f)",
					tc.merchant, tc.description, tc.wantCat, gotCat, gotConf)
			}
			if gotConf <= 0 {
				t.Errorf("confidence should be > 0 for matched merchant, got %.2f", gotConf)
			}
		})
	}
}

// TestCategorizeMerchantUnknown verifies that unknown merchants return "other" with zero confidence.
func TestCategorizeMerchantUnknown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		merchant    string
		description string
	}{
		{"zyxwvuts inc", ""},
		{"", ""},
		{"qwerty merchant llc", "misc purchase"},
		{"xyzzy", "xyzzy"},
	}

	for _, tc := range tests {
		t.Run(tc.merchant+"_"+tc.description, func(t *testing.T) {
			t.Parallel()
			gotCat, gotConf := CategorizeMerchant(tc.merchant, tc.description)
			if gotCat != "other" {
				t.Errorf("CategorizeMerchant(%q, %q): want %q, got %q", tc.merchant, tc.description, "other", gotCat)
			}
			if gotConf != 0.0 {
				t.Errorf("confidence for unknown merchant: want 0.0, got %.2f", gotConf)
			}
		})
	}
}

// TestCategorizeMerchantCaseInsensitive verifies that matching is not sensitive to case.
func TestCategorizeMerchantCaseInsensitive(t *testing.T) {
	t.Parallel()
	variants := []string{
		"STARBUCKS",
		"starbucks",
		"Starbucks",
		"StArBuCkS",
	}

	for _, v := range variants {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			got, _ := CategorizeMerchant(v, "")
			if got != "dining" {
				t.Errorf("CategorizeMerchant(%q): want %q, got %q", v, "dining", got)
			}
		})
	}
}

// TestCategorizeMerchantDescriptionFallback verifies that the description is
// used when the merchant is empty.
func TestCategorizeMerchantDescriptionFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		wantCat     string
	}{
		{"payroll direct deposit", "income"},
		{"netflix.com monthly", "entertainment"},
		{"whole foods market", "groceries"},
		{"uber eats delivery", "dining"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			got, _ := CategorizeMerchant("", tc.description)
			if got != tc.wantCat {
				t.Errorf("CategorizeMerchant(\"\", %q): want %q, got %q", tc.description, tc.wantCat, got)
			}
		})
	}
}

// TestCategorizeMerchantHighestConfidenceWins verifies that the highest-confidence
// rule wins when multiple rules could match.
func TestCategorizeMerchantHighestConfidenceWins(t *testing.T) {
	t.Parallel()
	// "doordash" matches the general dining rule AND the food delivery rule
	// which has higher confidence (0.95 vs 0.9). The higher one should win.
	cat, conf := CategorizeMerchant("doordash", "")
	if cat != "dining" {
		t.Errorf("want dining, got %q", cat)
	}
	if conf < 0.9 {
		t.Errorf("expected confidence >= 0.9 for doordash, got %.2f", conf)
	}
}

// TestCategoriesMapCompleteness verifies that every expected category key exists
// in the Categories map.
func TestCategoriesMapCompleteness(t *testing.T) {
	t.Parallel()
	required := []string{
		"income", "one_time_deposit", "housing", "debt_payment",
		"utilities", "groceries", "dining", "transport", "entertainment",
		"shopping", "health", "insurance", "subscriptions", "travel",
		"education", "personal", "pets", "gifts", "fees", "transfer", "other",
	}

	for _, key := range required {
		cat, ok := Categories[key]
		if !ok {
			t.Errorf("Categories[%q]: missing", key)
			continue
		}
		if cat.ID != key {
			t.Errorf("Categories[%q].ID: want %q, got %q", key, key, cat.ID)
		}
		if cat.Name == "" {
			t.Errorf("Categories[%q].Name: empty", key)
		}
	}
}

// TestCategorizeMerchantReturnValues verifies that the return values are always
// internally consistent (zero confidence = "other"; positive confidence = named cat).
func TestCategorizeMerchantReturnValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		merchant    string
		description string
	}{
		{"starbucks", ""},
		{"unknown-corp-xyz", ""},
		{"", ""},
		{"netflix", "streaming subscription"},
	}

	for _, tc := range cases {
		cat, conf := CategorizeMerchant(tc.merchant, tc.description)
		if cat == "" {
			t.Errorf("CategorizeMerchant(%q, %q): category must not be empty", tc.merchant, tc.description)
		}
		if cat == "other" && conf != 0.0 {
			t.Errorf("CategorizeMerchant(%q, %q): conf should be 0 for 'other', got %.2f", tc.merchant, tc.description, conf)
		}
		if cat != "other" && conf <= 0 {
			t.Errorf("CategorizeMerchant(%q, %q): conf should be > 0 for %q, got %.2f", tc.merchant, tc.description, cat, conf)
		}
	}
}

// TestCategorizeMerchantCombinedText verifies that merchant and description are
// combined during matching.
func TestCategorizeMerchantCombinedText(t *testing.T) {
	t.Parallel()
	// Neither "delivery" nor "door" alone should match dining, but "doordash delivery" should.
	cat, _ := CategorizeMerchant("delivery service", "doordash delivery")
	if cat != "dining" {
		t.Errorf("want dining for combined text, got %q", cat)
	}
}
