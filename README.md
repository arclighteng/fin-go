# fin

Local-first personal finance. Syncs with your bank via SimpleFIN or CSV import -- analyzes spending, detects subscriptions, catches unusual charges. All data stays on your machine.

**You are not the product.** No cloud. No tracking. No accounts.

[![Go](https://img.shields.io/badge/go-1.23%2B-00ADD8)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Features

### Security & Privacy
- **Local-only data** -- SQLite on your machine. No cloud sync, no telemetry, no phone home.
- **System keyring** -- Credentials stored in OS secure storage (Keychain, Credential Manager, Secret Service).
- **Single binary** -- Templates and static assets embedded via `go:embed`. Nothing to install, nothing to configure.

### Analysis
- **Smart categorization** -- Automatic transaction categorization with manual override.
- **Subscription detection** -- 150+ known services recognized instantly, plus pattern detection for the rest.
- **Alerts** -- Duplicate charges, unusual amounts, price increases, bundle overlap.
- **Spending breakdown** -- Top categories with 3-month rolling averages and outlier badges.
- **Cash flow tracking** -- Income vs expenses, savings rate, mid-month pacing.

### Interface
- **Web dashboard** -- Full-featured UI at localhost with drilldown into every number.
- **CLI tools** -- Complete command-line interface for automation and scripting.
- **Mobile responsive** -- Works on phone, tablet, and desktop.
- **Dark mode** -- Easy on the eyes.

## Getting Started

### Download a release

Grab the latest binary for your platform from [Releases](https://github.com/arclighteng/fin-go/releases):

```bash
# Run the web dashboard
./fin web
# Browser opens to http://127.0.0.1:8000/dashboard
```

> **First run:** On first launch with no data, the dashboard shows an empty state with a banner to import a CSV or connect SimpleFIN. To explore with sample data first, run `fin demo load` -- this loads demo transactions with a dismissible banner so you can see every feature before connecting your bank.

### Build from source

```bash
git clone https://github.com/arclighteng/fin-go.git
cd fin-go
go build -o fin ./cmd/fin
./fin web
```

To bake in the version string:

```bash
go build -ldflags "-s -w -X main.version=0.1.1" -o fin ./cmd/fin
```

The database is stored at:
- **Windows**: `%APPDATA%\fin\fin.db`
- **macOS/Linux**: `~/.local/share/fin/fin.db`

Override with `FIN_DB_PATH` if needed.

## Connecting Your Bank

From the dashboard, click **"Connect your bank"** to open the setup page (`/connect`). Two options:

**CSV import** (easiest -- no account required)
1. Download a transaction export from your bank (CSV)
2. Drag and drop it onto the import page
3. Automatic format detection for Chase, BofA, Amex, Wells Fargo, and Capital One

**SimpleFIN** (automatic daily sync, ~$1.50/month)
1. Go to [SimpleFIN Bridge](https://beta-bridge.simplefin.org/), subscribe, and link your bank
2. Copy your Setup Token
3. Paste it into the SimpleFIN section on the connect page

## Web Dashboard

Launch with `fin web`. The dashboard at `/dashboard` shows a 5-card layout:

| Card | What it shows |
|------|---------------|
| **Cash Flow** | Income vs expenses, savings rate, 3-month comparison, mid-month pacing |
| **Commitments** | Detected subscriptions and bills, total as % of income, price change alerts |
| **Spending Breakdown** | Top 7 categories with bars, 3-month averages, outlier badges |
| **Heads Up** | Unusual charges with dismiss actions, spending trends, bill deviation alerts |
| **Your Trend** | 6-month bar chart of net cash flow, clickable months |

Click any number, bar, category, or merchant to drill down to the full transaction list.

### Other Pages

| Route | Purpose |
|-------|---------|
| `/connect` | Import CSV files or connect via SimpleFIN |
| `/commitments` | Subscriptions and bills -- filter, export, toggle types |
| `/insights` | 12-month savings and income trends |
| `/review` | Transaction triage and categorization |
| `/budget` | Spending targets by category vs actual |
| `/sync-log` | Sync history |

### Navigation
- **Month navigation**: Previous/next with current month indicator
- **Account filter**: Multi-select to focus on specific accounts
- **Transaction search**: Live results -- type a merchant name or amount
- **Keyboard accessible**: Tab navigation, Enter to select, Escape to close

## Subscription Detection

### Known Services (instant)

150+ services recognized from a single charge:
- **Streaming**: Netflix, Hulu, Disney+, Max, YouTube TV, Spotify, Apple Music
- **Software**: Adobe, Microsoft 365, GitHub, ChatGPT, 1Password
- **Fitness**: Peloton, Planet Fitness, Strava
- **And more** -- VPN, news, gaming, home security, cloud storage

### Pattern Detection (3+ charges)

Unknown merchants are detected via consistent amounts, regular intervals, and recurring payment indicators.

## Security

Your financial data is sensitive. fin eliminates entire threat categories by design.

### What we don't do
- Cloud storage or remote API calls
- User accounts, login tokens, or session cookies
- Telemetry, analytics, or phone home -- ever

### What we delegate
- **Credentials** -> OS keyring (Keychain, Credential Manager, Secret Service)
- **Disk encryption** -> BitLocker / FileVault / LUKS

We didn't build our own crypto -- that's the point.

### Credential Storage

```bash
fin setup ACCESS_URL    # Store SimpleFIN URL in system keyring
fin health              # Check connection status
```

Credentials are stored in the OS keyring by default. Alternative: create a `.env` file (gitignored) with `SIMPLEFIN_ACCESS_URL`. Keyring takes priority if both are configured.

## CLI Reference

### Sync

| Command | Description |
|---------|-------------|
| `fin sync` | Pull latest transactions (30 days, default) |
| `fin sync --lookback 14` | Quick sync -- 14 days |
| `fin sync --lookback 120` | Full sync -- 120 days |

### Dashboard

| Command | Description |
|---------|-------------|
| `fin web` | Start the web dashboard |
| `fin web --host 0.0.0.0` | Listen on all interfaces (LAN access) |
| `fin web --port 9000` | Use a custom port |

### Setup & Diagnostics

| Command | Description |
|---------|-------------|
| `fin setup ACCESS_URL` | Set up SimpleFIN connection |
| `fin health` | Check system and SimpleFIN connection status |
| `fin status` | Show account balances and sync status |
| `fin demo load` | Load demo data for testing |
| `fin demo clear` | Remove demo data |

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `SIMPLEFIN_ACCESS_URL` | SimpleFIN API access URL (fallback; prefer keyring) | none |
| `FIN_DB_PATH` | SQLite database path | platform-specific |
| `FIN_LOG_LEVEL` | Logging level | `INFO` |
| `FIN_LOG_FORMAT` | Log format (`simple` or `json`) | `simple` |
| `FIN_TZ` | IANA timezone for display | `UTC` |

## Embedding

fin is designed to be embedded in other applications. The `pkg/fincore` package provides the public API:

```go
import "github.com/arclighteng/fin-go/pkg/fincore"

cfg := fincore.LoadConfig()
cfg.Version = "1.0.0"

srv, err := fincore.NewServer(cfg)
if err != nil { log.Fatal(err) }
defer srv.Close()

// Wrap srv with auth middleware, then serve.
http.ListenAndServe(":8000", authMiddleware(srv))
```

## Troubleshooting

### "No transactions found"
Run `fin sync --lookback 120` to pull more history, or import a CSV from `/connect`.

### Categories are wrong
Click the category in the dashboard, then click the edit icon to override.

### Subscription showing as bill (or vice versa)
Click the type badge on the Commitments page to toggle.

## Contributing

Bug reports and feature requests are welcome via [GitHub Issues](https://github.com/arclighteng/fin-go/issues). Pull requests are considered -- open an issue first to discuss the change. All contributions are licensed under [MIT](LICENSE).

## Development

```bash
go build ./...
go test ./...
go vet ./...
```

## License

[MIT](LICENSE)
