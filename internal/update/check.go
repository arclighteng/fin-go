// Package update checks GitHub Releases for newer versions of fin.
//
// Results are cached in memory for 24 hours so the check does not
// hammer the GitHub API on every application start.
package update

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// UpdateInfo holds the result of a version check.
type UpdateInfo = Info

// Info holds version comparison data returned by Check.
type Info struct {
	// Current is the running version string (without a "v" prefix).
	Current string

	// Latest is the most recent release tag on GitHub (without a "v" prefix).
	Latest string

	// HasUpdate is true when Latest differs from Current and is non-empty.
	HasUpdate bool

	// ReleaseURL is the HTML URL of the GitHub release page.
	ReleaseURL string

	// CheckedAt is when the GitHub API was actually queried.
	CheckedAt time.Time
}

// cache holds the most recent check result so repeated calls within 24 hours
// skip the network round-trip.
var cache struct {
	mu        sync.Mutex
	result    *Info
	expiresAt time.Time
}

const cacheTTL = 24 * time.Hour

// Check queries the GitHub Releases API for the latest fin release and
// compares it against currentVersion (no "v" prefix expected).
//
// The call is best-effort: a 3-second timeout is imposed and network or parse
// errors return nil. Callers must nil-check the result.
//
// Results are cached for 24 hours; subsequent calls within that window return
// the cached value immediately without hitting GitHub.
func Check(ctx context.Context, currentVersion string) *Info {
	cache.mu.Lock()
	if cache.result != nil && time.Now().Before(cache.expiresAt) {
		cached := *cache.result
		cache.mu.Unlock()
		return &cached
	}
	cache.mu.Unlock()

	info := fetch(ctx, currentVersion)
	if info == nil {
		return nil
	}

	cache.mu.Lock()
	cache.result = info
	cache.expiresAt = time.Now().Add(cacheTTL)
	cache.mu.Unlock()

	return info
}

// InvalidateCache clears the in-memory cache, forcing the next Check call to
// query GitHub. Useful in tests.
func InvalidateCache() {
	cache.mu.Lock()
	cache.result = nil
	cache.expiresAt = time.Time{}
	cache.mu.Unlock()
}

// fetch performs the actual HTTP request to the GitHub Releases API.
func fetch(ctx context.Context, currentVersion string) *Info {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		"https://api.github.com/repos/arclighteng/fin/releases/latest", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	return &Info{
		Current:    currentVersion,
		Latest:     latest,
		HasUpdate:  latest != "" && latest != currentVersion,
		ReleaseURL: release.HTMLURL,
		CheckedAt:  time.Now().UTC(),
	}
}
