// Package config reads the service settings from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	// Listen is the address the HTTP server binds, e.g. ":8810".
	Listen string
	// BasePath is the path prefix the reverse proxy routes to us, e.g.
	// "/ext/requests". Every route is served below it.
	BasePath string
	// DBPath is the SQLite database file.
	DBPath string

	// RommURL is RomM's base URL as this service reaches it (no trailing slash).
	RommURL string
	// RommToken is a RomM Client API Token (roms.read, platforms.read).
	RommToken string
	// AuthCacheTTL is how long a validated session cookie is trusted.
	AuthCacheTTL time.Duration

	IGDBClientID     string
	IGDBClientSecret string

	// Download provider and import (defaults; the settings page can change
	// the provider part at run time).
	TTBURL      string
	TTBStorage  string // TTB storage the downloads land in (staging)
	StagingRoot string // that storage as mounted in this container
	LibraryRoot string // the platform folders (RomM's roms/<fs_slug>)
	ScanHook    string // romm-scan-hook URL

	NtfyURL   string // admin topic URL; empty = no notifications
	NtfyToken string
	PublicURL string // where a notification tap leads (the Demandes page)
}

func FromEnv() (Config, error) {
	c := Config{
		Listen:           env("LISTEN", ":8810"),
		BasePath:         strings.TrimRight(env("BASE_PATH", "/ext/requests"), "/"),
		DBPath:           env("DB_PATH", "/data/requests.db"),
		RommURL:          strings.TrimRight(os.Getenv("ROMM_URL"), "/"),
		RommToken:        os.Getenv("ROMM_TOKEN"),
		AuthCacheTTL:     time.Minute,
		IGDBClientID:     os.Getenv("IGDB_CLIENT_ID"),
		IGDBClientSecret: os.Getenv("IGDB_CLIENT_SECRET"),
		TTBURL:           strings.TrimRight(os.Getenv("TTB_URL"), "/"),
		TTBStorage:       env("TTB_STORAGE", "roms-staging"),
		StagingRoot:      env("STAGING_ROOT", "/staging"),
		LibraryRoot:      env("LIBRARY_ROOT", "/library"),
		ScanHook:         os.Getenv("SCAN_HOOK_URL"),
		NtfyURL:          os.Getenv("NTFY_URL"),
		NtfyToken:        os.Getenv("NTFY_TOKEN"),
		PublicURL:        os.Getenv("PUBLIC_URL"),
	}
	var missing []string
	for name, value := range map[string]string{
		"ROMM_URL": c.RommURL, "ROMM_TOKEN": c.RommToken,
		"IGDB_CLIENT_ID": c.IGDBClientID, "IGDB_CLIENT_SECRET": c.IGDBClientSecret,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing environment: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
