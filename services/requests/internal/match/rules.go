// Package match ranks release titles against a requested game, under rules
// set per platform.
package match

// Rules decide how a release is scored and whether it may be taken without a
// human. One set per platform; DefaultRules gives the built-in values.
type Rules struct {
	// Regions, most wanted first. Tokens matched in the release name
	// (case-insensitive): "fr", "eu", "multi", "us", "jp", "world".
	Regions []string `json:"regions"`
	// Formats wanted, best first (file extensions or container words such as
	// "chd", "iso", "7z"). An unknown format scores lower; Deny rejects.
	Formats []string `json:"formats"`
	Deny    []string `json:"deny"`
	// Plausible size of one game. Outside the range rejects; above SetSize
	// (a full set or a compilation) is never taken automatically.
	MinSize int64 `json:"min_size"`
	MaxSize int64 `json:"max_size"`
	SetSize int64 `json:"set_size"`
	// MinSeeders for an automatic pick.
	MinSeeders int `json:"min_seeders"`
	// Convert discs after import ("chd" for psx/ps2; empty = keep as is).
	Convert string `json:"convert,omitempty"`
}

const (
	kb = int64(1) << 10
	mb = int64(1) << 20
	gb = int64(1) << 30
)

var defaultRegions = []string{"fr", "eu", "multi", "world", "us", "jp"}

// cartridge returns rules for a cartridge platform with the given size range.
func cartridge(exts []string, min, max int64) Rules {
	return Rules{Regions: defaultRegions, Formats: append(exts, "7z", "zip"), Deny: []string{"exe", "rar"},
		MinSize: min, MaxSize: max, SetSize: max * 4, MinSeeders: 1}
}

// DefaultRules per RomM platform folder (fs_slug). A platform not listed
// gets the "default" entry.
func DefaultRules() map[string]Rules {
	disc := func(exts []string, min, max int64, convert string) Rules {
		return Rules{Regions: defaultRegions, Formats: exts, Deny: []string{"exe"},
			MinSize: min, MaxSize: max, SetSize: max * 3, MinSeeders: 1, Convert: convert}
	}
	return map[string]Rules{
		"default":      {Regions: defaultRegions, Formats: []string{"7z", "zip", "iso"}, Deny: []string{"exe"}, MinSize: 16 * kb, MaxSize: 50 * gb, SetSize: 100 * gb, MinSeeders: 1},
		"gb":           cartridge([]string{"gb"}, 16*kb, 8*mb),
		"gbc":          cartridge([]string{"gbc", "gb"}, 16*kb, 8*mb),
		"gba":          cartridge([]string{"gba"}, 64*kb, 64*mb),
		"nes":          cartridge([]string{"nes"}, 8*kb, 4*mb),
		"snes":         cartridge([]string{"sfc", "smc"}, 64*kb, 12*mb),
		"n64":          cartridge([]string{"z64", "n64", "v64"}, 1*mb, 128*mb),
		"n64dd":        cartridge([]string{"ndd"}, 1*mb, 128*mb),
		"nds":          cartridge([]string{"nds"}, 1*mb, 1*gb),
		"3ds":          cartridge([]string{"3ds", "cci", "cia"}, 16*mb, 8*gb),
		"genesis":      cartridge([]string{"md", "gen", "bin", "smd"}, 64*kb, 16*mb),
		"mastersystem": cartridge([]string{"sms"}, 8*kb, 4*mb),
		"sega32x":      cartridge([]string{"32x"}, 256*kb, 8*mb),
		// No conversion by default: chdman over NFS on the NUC saturates it
		// (first PS2 import, 2026-10-03). The settings page can turn it on.
		"psx": disc([]string{"chd", "pbp", "cue", "bin", "iso", "7z", "zip"}, 5*mb, 3*gb, ""),
		"ps2": disc([]string{"chd", "iso", "7z", "zip"}, 50*mb, 16*gb, ""),
		"psp": disc([]string{"cso", "iso", "chd", "7z", "zip"}, 5*mb, 4*gb, ""),
		"gc":  disc([]string{"rvz", "iso", "gcm", "ciso", "7z", "zip"}, 50*mb, 2*gb, ""),
		"wii": disc([]string{"rvz", "wbfs", "iso", "7z", "zip"}, 50*mb, 9*gb, ""),
	}
}

// For returns the rules for a platform from a rule set, falling back to
// "default" and then to the built-in defaults.
func For(set map[string]Rules, platform string) Rules {
	if r, ok := set[platform]; ok {
		return r
	}
	if r, ok := DefaultRules()[platform]; ok {
		return r
	}
	if r, ok := set["default"]; ok {
		return r
	}
	return DefaultRules()["default"]
}
