package credentials

import (
	"log"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	serviceName  = "fin"
	simpleFinKey = "simplefin_access_url"
)

var (
	keyringOnce      sync.Once
	keyringAvailable bool
)

func isKeyringAvailable() bool {
	keyringOnce.Do(func() {
		// Probe by attempting a get. If the backend is broken, this fails.
		_, err := keyring.Get(serviceName, "__probe__")
		// ErrNotFound is fine -- it means the backend works, just no value stored.
		keyringAvailable = err == nil || err == keyring.ErrNotFound
		if !keyringAvailable {
			log.Printf("keyring not available: %v", err)
		}
	})
	return keyringAvailable
}

// GetCredential retrieves a credential from the system keyring.
func GetCredential(key string) (string, error) {
	if !isKeyringAvailable() {
		return "", nil
	}
	val, err := keyring.Get(serviceName, key)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	return val, err
}

// SetCredential stores a credential in the system keyring.
func SetCredential(key, value string) error {
	if !isKeyringAvailable() {
		return nil
	}
	return keyring.Set(serviceName, key, value)
}

// DeleteCredential removes a credential from the system keyring.
func DeleteCredential(key string) error {
	if !isKeyringAvailable() {
		return nil
	}
	err := keyring.Delete(serviceName, key)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// GetSimpleFinURL returns the SimpleFIN access URL from the keyring.
func GetSimpleFinURL() (string, error) {
	return GetCredential(simpleFinKey)
}

// SetSimpleFinURL stores the SimpleFIN access URL.
func SetSimpleFinURL(url string) error {
	return SetCredential(simpleFinKey, strings.TrimSpace(url))
}

// ClearSimpleFinURL removes the SimpleFIN access URL.
func ClearSimpleFinURL() error {
	return DeleteCredential(simpleFinKey)
}

// GetCredentialSource returns where credentials are loaded from: "keyring", "env", or "none".
func GetCredentialSource() string {
	if url, _ := GetSimpleFinURL(); url != "" {
		return "keyring"
	}
	return "none" // env check happens at config level
}
