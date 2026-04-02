package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for fin.
type Config struct {
	SimpleFinAccessURL string
	DBPath             string
	LogLevel           string
	LogFormat          string
	Timezone           string
	// Version is the application version string (e.g. "1.2.3" or "dev").
	// It is injected by the embedding application or CLI at startup.
	Version string
}

func Load() *Config {
	// Load .env from current directory (best-effort)
	godotenv.Load()

	return &Config{
		SimpleFinAccessURL: getSimpleFinURL(),
		DBPath:             envOrDefault("FIN_DB_PATH", defaultDBPath()),
		LogLevel:           strings.ToUpper(envOrDefault("FIN_LOG_LEVEL", "INFO")),
		LogFormat:          strings.ToLower(envOrDefault("FIN_LOG_FORMAT", "simple")),
		Timezone:           envOrDefault("FIN_TZ", "UTC"),
	}
}

func getSimpleFinURL() string {
	// Try keyring first
	if url, err := credentials.GetSimpleFinURL(); err == nil && url != "" {
		return url
	}
	return strings.TrimSpace(os.Getenv("SIMPLEFIN_ACCESS_URL"))
}

func defaultDBPath() string {
	// Docker detection
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "/app/data/fin.db"
	}
	if wd, _ := os.Getwd(); wd == "/app" {
		return "/app/data/fin.db"
	}

	switch runtime.GOOS {
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			home, _ := os.UserHomeDir()
			appdata = home
		}
		return filepath.Join(appdata, "fin", "fin.db")
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			home, _ := os.UserHomeDir()
			xdg = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(xdg, "fin", "fin.db")
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// EnsureDataDir creates the parent directory of the DB path and hardens permissions.
func EnsureDataDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	hardenDir(dir)
	return nil
}
