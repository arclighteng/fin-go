package classify

// This file contains the single canonical keyword lists for transfer and
// income detection. Both classifier.go and transfers.go reference these
// slices directly; there are no duplicates elsewhere in the package.

// transferKeywords matches P2P apps, ACH, wire, and similar transfer signals.
// Used by both the classifier (substring match) and the transfer pairing
// scorer (substring match for keyword bonus).
var transferKeywords = []string{
	"transfer", "xfer", "ach", "wire", "zelle", "venmo", "paypal",
	"cash app", "cashapp", "apple cash", "square cash", "wisely",
	"online banking transfer", "mobile deposit",
}

// bankKeywords identifies financial institutions.
// The classifier uses a simple substring match; the transfer pairing scorer
// compiles these into word-boundary regexps (see transfers.go).
var bankKeywords = []string{
	"chase", "wells fargo", "bank of america", "bofa", "citi", "citibank",
	"capital one", "us bank", "pnc", "td bank", "ally", "discover",
	"american express", "amex", "barclays", "synchrony", "marcus",
	"fidelity", "schwab", "vanguard", "betterment", "wealthfront",
	"savings", "checking", "brokerage",
}

// ccPaymentKeywords identifies credit card payment descriptions.
var ccPaymentKeywords = []string{
	"payment thank you", "autopay", "online payment", "payment received",
	"credit card payment", "cc payment", "automatic payment",
}

// payrollKeywords identifies payroll / direct deposit transactions.
var payrollKeywords = []string{
	"payroll", "salary", "wages", "direct dep", "direct deposit",
	"pay from", "paycheck", "employer", "adp", "paychex", "gusto", "workday",
	"quickbooks payroll", "zenefits",
}

// refundKeywords identifies refund / credit transactions.
var refundKeywords = []string{
	"refund", "credit", "return", "reversal", "chargeback",
	"adjustment", "rebate", "cashback",
}

// bankDirectionWords are words that, combined with a bank name, indicate a
// bank-to-bank transfer. Kept separate so the logic is explicit.
var bankDirectionWords = []string{"transfer", "to", "from"}
