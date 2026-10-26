package match

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Release is what a provider reports about one search result.
type Release struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Size     int64  `json:"size"`
	Seeders  int    `json:"seeders"`
	Indexer  string `json:"indexer"`
	InfoHash string `json:"info_hash,omitempty"`
	Usable   bool   `json:"usable"` // has a magnet or a torrent
}

// Verdict of one release.
const (
	Auto      = "auto"      // safe to download without asking
	Candidate = "candidate" // plausible, a human decides
	Reject    = "reject"    // not this game, or not acceptable
)

type Scored struct {
	Release
	Score     float64  `json:"score"`
	Verdict   string   `json:"verdict"`
	TitleKey  string   `json:"title_key"`
	Platforms []string `json:"platforms"`
	Region    string   `json:"region,omitempty"`
	Format    string   `json:"format,omitempty"`
	Reasons   []string `json:"reasons"` // why it scored what it did, for the settings page
}

// Wanted describes the request a release is checked against.
type Wanted struct {
	Title    string
	AltNames []string
	Platform string // fs_slug
	Rules    Rules
	Library  []PlatformInfo // every library platform, for alias detection
}

type PlatformInfo struct {
	Slug string
	Name string
}

// aliases: how release names spell each platform, by RomM fs_slug. The
// platform's own name is added at runtime.
var aliases = map[string][]string{
	"3ds": {"3ds", "nintendo 3ds"}, "gb": {"gb", "game boy", "gameboy"},
	"gba": {"gba", "game boy advance", "gameboy advance"}, "gbc": {"gbc", "game boy color", "gameboy color"},
	"gc": {"gamecube", "ngc", "gc", "nintendo gamecube"}, "genesis": {"genesis", "mega drive", "megadrive", "md"},
	"mastersystem": {"master system", "sms", "mark iii"}, "n64": {"n64", "nintendo 64"},
	"n64dd": {"64dd", "n64dd"}, "nds": {"nds", "nintendo ds", "ds"},
	"nes": {"nes", "famicom", "nintendo entertainment system"}, "ps2": {"ps2", "playstation 2"},
	"psp": {"psp", "playstation portable"}, "psx": {"psx", "ps1", "psone", "playstation", "playstation 1"},
	"sega32x": {"32x", "sega 32x"}, "snes": {"snes", "super nintendo", "super famicom", "sfc"},
	"switch": {"switch", "nsw", "nintendo switch"}, "wii": {"wii"}, "wiiu": {"wii u", "wiiu"},
}

// noise: words a release adds that say nothing about which game it is. No
// digits (sequels), no publisher names ("Sega Rally" must not become "Rally").
var noise = toSet("rom roms iso cso chd cia nsp xci wbfs rvz zip 7z rar usa eur europe pal ntsc jpn japan fra french eng english multi multi2 multi3 multi4 multi5 multi6 vf vff vostfr proper repack decrypted encrypted nointro redump")

// tagWords may fill a bracketed group that is then dropped: "(Europe) (En,Fr)".
var tagWords = toSet("u e j k a usa us eur europe japan jpn world asia korea kor uk en fr de es it nl pt sv no da fi pl ru rus eng fre ger spa ita jap ntsc pal rev beta proto unl hack translated t b h f o a1 " +
	"france germany spain italy netherlands sweden australia brazil canada china taiwan hong kong scandinavia")

var unwanted = []string{"soundtrack", "ost", "artbook", "art book", "guide", "walkthrough", "trainer", "cheat", "cheats",
	"dlc", "update", "patch", "demo", "manual", "scan", "video", "movie", "wallpaper", "theme", "save", "collection", "pack", "bundle"}

var stopWords = toSet("the a an")
var roman = map[string]string{"ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9"}

func toSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var (
	nonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
	htmlEntity = regexp.MustCompile(`&[a-z]+;`)
	brackets   = regexp.MustCompile(`\(([^)]*)\)|\[([^\]]*)\]`)
	sceneGroup = regexp.MustCompile(`-[A-Za-z0-9]+$`)
	digitsOnly = regexp.MustCompile(`^v?\d+$`)
)

var stripMarks = transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// words lowercases, strips accents and punctuation, and pads with spaces so
// " alias " searches match whole words.
func words(s string) string {
	s, _, _ = transform.String(stripMarks, s)
	s = strings.ToLower(s)
	s = htmlEntity.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "’", "")
	s = nonAlnum.ReplaceAllString(s, " ")
	return " " + strings.Join(strings.Fields(s), " ") + " "
}

// Key folds a title for comparison: accents, case, punctuation, articles,
// roman numerals (no "x": Mega Man X is not Mega Man 10).
func Key(title string) string {
	var out []string
	for _, w := range strings.Fields(words(title)) {
		if stopWords[w] {
			continue
		}
		if r, ok := roman[w]; ok {
			w = r
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

type alias struct{ slug, text string }

func aliasList(library []PlatformInfo) []alias {
	var list []alias
	seen := map[string]bool{}
	add := func(slug, text string) {
		text = strings.TrimSpace(words(text))
		if text != "" && !seen[slug+"|"+text] {
			seen[slug+"|"+text] = true
			list = append(list, alias{slug, text})
		}
	}
	for _, p := range library {
		for _, a := range aliases[p.Slug] {
			add(p.Slug, a)
		}
		if len(aliases[p.Slug]) == 0 {
			add(p.Slug, p.Slug)
		}
		add(p.Slug, p.Name)
	}
	// Longest first, so "playstation portable" is consumed before "playstation".
	sort.SliceStable(list, func(i, j int) bool { return len(list[i].text) > len(list[j].text) })
	return list
}

// DetectPlatforms names the platforms a release title mentions.
func detectPlatforms(title string, list []alias) []string {
	text := words(title)
	found := []string{}
	for _, a := range list {
		needle := " " + a.text + " "
		if !strings.Contains(text, needle) {
			continue
		}
		if !slices.Contains(found, a.slug) {
			found = append(found, a.slug)
		}
		text = strings.ReplaceAll(text, needle, " ")
	}
	return found
}

// releaseKey reduces a release title to the game's name.
func releaseKey(title string, list []alias) string {
	raw := title
	if !strings.ContainsAny(raw, " \t") {
		raw = sceneGroup.ReplaceAllString(raw, "")
	}
	raw = strings.NewReplacer("_", " ", ".", " ").Replace(raw)
	aliasWords := map[string]bool{}
	for _, a := range list {
		for _, w := range strings.Fields(a.text) {
			aliasWords[w] = true
		}
	}
	raw = brackets.ReplaceAllStringFunc(raw, func(group string) string {
		square := strings.HasPrefix(group, "[")
		inner := strings.TrimSpace(words(group[1 : len(group)-1]))
		if square && !strings.Contains(inner, " ") {
			return " " // [uploader]
		}
		for _, w := range strings.Fields(inner) {
			if !(tagWords[w] || noise[w] || aliasWords[w] || digitsOnly.MatchString(w)) {
				return " " + inner + " " // "(FULL PS1 Collection)" stays
			}
		}
		return " "
	})
	text := words(raw)
	for _, a := range list {
		text = strings.ReplaceAll(text, " "+a.text+" ", " ")
	}
	var kept []string
	for _, w := range strings.Fields(text) {
		if !noise[w] {
			kept = append(kept, w)
		}
	}
	return Key(strings.Join(kept, " "))
}

// regionTokens maps what a release name says to the rule vocabulary.
var regionTokens = map[string][]string{
	"fr":    {"fr", "fra", "french", "vf", "vff", "france", "francais"},
	"eu":    {"eur", "europe", "e", "pal"},
	"multi": {"multi", "multi2", "multi3", "multi4", "multi5", "multi6"},
	"world": {"world", "w"},
	"us":    {"usa", "us", "u", "ntsc"},
	"jp":    {"jpn", "japan", "j", "jap"},
}

func detectRegion(title string, order []string) string {
	text := words(title)
	for _, region := range order {
		for _, tok := range regionTokens[region] {
			if strings.Contains(text, " "+tok+" ") {
				return region
			}
		}
	}
	return ""
}

func detectFormat(title string, formats, deny []string) (string, bool) {
	text := words(title)
	for _, f := range deny {
		if strings.Contains(text, " "+f+" ") {
			return f, true
		}
	}
	for _, f := range formats {
		if strings.Contains(text, " "+f+" ") {
			return f, false
		}
	}
	return "", false
}

// Rank scores every release and sorts the plausible ones best first.
// Rejected releases are dropped unless keepRejected (the test-a-title view).
func Rank(releases []Release, w Wanted, keepRejected bool) []Scored {
	list := aliasList(w.Library)
	gameKeys := []string{releaseKey(w.Title, list)}
	// "Pokémon Emerald Version" is released as "Pokemon Emerald": a trailing
	// "Version"/"Edition" is neutral. The subtitle is not ("Golden Sun" is
	// not "Golden Sun: Dark Dawn").
	names := append([]string{trailingWords.ReplaceAllString(strings.TrimSpace(w.Title), "")}, w.AltNames...)
	for _, alt := range names {
		if k := releaseKey(alt, list); k != "" && !slices.Contains(gameKeys, k) {
			gameKeys = append(gameKeys, k)
		}
	}
	var out []Scored
	for _, r := range releases {
		s := score(r, w, list, gameKeys)
		if s.Verdict == Reject && !keepRejected {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		vi, vj := out[i].Verdict == Auto, out[j].Verdict == Auto
		if vi != vj {
			return vi
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Seeders > out[j].Seeders
	})
	return out
}

func score(r Release, w Wanted, list []alias, gameKeys []string) Scored {
	s := Scored{Release: r, Platforms: detectPlatforms(r.Title, list), Reasons: []string{}}
	s.TitleKey = releaseKey(r.Title, list)
	text := words(r.Title)
	reject := func(why string) Scored {
		s.Verdict, s.Reasons = Reject, append(s.Reasons, why)
		return s
	}

	title := "none"
	for _, k := range gameKeys {
		if k == "" {
			continue
		}
		if s.TitleKey == k {
			title = "exact"
			break
		}
		if strings.Contains(" "+s.TitleKey+" ", " "+k+" ") {
			title = "contains"
		}
	}
	switch title {
	case "none":
		return reject("title does not match")
	case "exact":
		s.Score += 100
		s.Reasons = append(s.Reasons, "exact title")
	case "contains":
		s.Score += 40
		s.Reasons = append(s.Reasons, "title contains the game's name (sequel, compilation?)")
	}

	platformOK := slices.Contains(s.Platforms, w.Platform)
	switch {
	case len(s.Platforms) > 0 && !platformOK:
		return reject("names another platform: " + strings.Join(s.Platforms, ", "))
	case platformOK && len(s.Platforms) == 1:
		s.Score += 40
		s.Reasons = append(s.Reasons, "names the platform")
	case platformOK:
		s.Score += 10
		s.Reasons = append(s.Reasons, "names several platforms")
	default:
		s.Reasons = append(s.Reasons, "no platform named")
	}

	for _, word := range unwanted {
		if strings.Contains(text, " "+word+" ") {
			s.Score -= 60
			s.Reasons = append(s.Reasons, "contains \""+word+"\"")
			title = "unwanted"
			break
		}
	}

	format, denied := detectFormat(r.Title, w.Rules.Formats, w.Rules.Deny)
	if denied {
		return reject("denied format " + format)
	}
	if format != "" {
		s.Format = format
		s.Score += float64(len(w.Rules.Formats)-slices.Index(w.Rules.Formats, format)) * 2
	}

	s.Region = detectRegion(r.Title, w.Rules.Regions)
	if s.Region != "" {
		// Region outweighs popularity: a French release with 2 seeders beats
		// a US one with 50.
		s.Score += float64(len(w.Rules.Regions)-slices.Index(w.Rules.Regions, s.Region)) * 6
		s.Reasons = append(s.Reasons, "region "+s.Region)
	}

	if r.Size > 0 && w.Rules.MaxSize > 0 && (r.Size < w.Rules.MinSize || r.Size > w.Rules.SetSize) {
		return reject("implausible size")
	}
	set := r.Size > w.Rules.MaxSize && w.Rules.MaxSize > 0
	if set {
		s.Reasons = append(s.Reasons, "larger than one game (a set?)")
		s.Score -= 30
	}
	if !r.Usable {
		return reject("no magnet or torrent")
	}
	s.Score += float64(min(r.Seeders, 50)) / 2

	s.Verdict = Candidate
	if title == "exact" && platformOK && len(s.Platforms) == 1 && !set &&
		r.Seeders >= max(w.Rules.MinSeeders, 1) && s.Region != "jp" {
		s.Verdict = Auto
	}
	return s
}

// BestFile picks, among file names of one download, the one whose region
// ranks best under the rules (ties: the first, names sorted).
func BestFile(names []string, r Rules) int {
	best, bestRank := 0, len(r.Regions)+1
	for i, n := range names {
		region := detectRegion(n, r.Regions)
		rank := len(r.Regions)
		if region != "" {
			rank = slices.Index(r.Regions, region)
		}
		if rank < bestRank {
			best, bestRank = i, rank
		}
	}
	return best
}

var (
	trailingWords = regexp.MustCompile(`(?i)\s+(version|edition)$`)
	latin         = regexp.MustCompile(`^[\p{Latin}\p{N}\p{P}\p{Zs}\p{S}]+$`)
)

// QueryText makes a title fit for a tracker search: accents removed,
// punctuation as spaces ("Pokémon : Version Émeraude" -> "Pokemon Version
// Emeraude"), case kept.
func QueryText(title string) string {
	s, _, _ := transform.String(stripMarks, title)
	s = strings.NewReplacer("&", " and ", "'", "", "’", "").Replace(s)
	s = regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// Queries lists what to search for a game, best first, at most max: the main
// title, its short form (no subtitle, no trailing "Version"/"Edition"), then
// the alternative names in the order given (English, spelling variants,
// French first). Names in non-Latin scripts are skipped; duplicates after
// normalisation are dropped.
func Queries(title string, alts []string, max int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if len(out) >= max || name == "" || !latin.MatchString(name) {
			return
		}
		q := QueryText(name)
		if q == "" || seen[strings.ToLower(q)] {
			return
		}
		seen[strings.ToLower(q)] = true
		out = append(out, q)
	}
	add(title)
	short := title
	if i := strings.IndexAny(short, ":-–"); i > 3 {
		short = short[:i]
	}
	add(trailingWords.ReplaceAllString(strings.TrimSpace(short), ""))
	for _, alt := range alts {
		add(alt)
	}
	return out
}
