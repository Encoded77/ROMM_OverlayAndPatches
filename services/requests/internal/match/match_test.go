package match

import (
	"strings"
	"testing"
)

var library = []PlatformInfo{
	{"gba", "Game Boy Advance"}, {"nds", "Nintendo DS"}, {"psx", "PlayStation"},
	{"psp", "PlayStation Portable"}, {"snes", "Super Nintendo Entertainment System"},
	{"n64", "Nintendo 64"}, {"gc", "Nintendo GameCube"},
}

func want(title, platform string) Wanted {
	return Wanted{Title: title, Platform: platform, Rules: For(nil, platform), Library: library}
}

func rel(title string, seeders int, size int64) Release {
	return Release{ID: title, Title: title, Seeders: seeders, Size: size, Usable: true}
}

func verdict(t *testing.T, w Wanted, r Release) Scored {
	t.Helper()
	got := Rank([]Release{r}, w, true)
	if len(got) != 1 {
		t.Fatalf("no result for %q", r.Title)
	}
	return got[0]
}

func TestKey(t *testing.T) {
	cases := map[string]string{
		"Final Fantasy IV (USA)":                    "final fantasy 4 usa",
		"Legend of Zelda, The - A Link to the Past": "legend of zelda link to past",
		"The Legend of Zelda: A Link to the Past":   "legend of zelda link to past",
		"Pokémon Émeraude":                          "pokemon emeraude",
		"Ozzy & Drix":                               "ozzy and drix",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
	if Key("Mega Man X") == Key("Mega Man 10") {
		t.Error("X must not become 10")
	}
}

func TestVerdicts(t *testing.T) {
	const m = int64(1) << 20
	cases := []struct {
		title, platform, release string
		seeders                  int
		size                     int64
		want                     string
	}{
		{"Golden Sun: Dark Dawn", "nds", "Golden Sun Dark Dawn USA PROPER NDS [emolson]", 7, 134 * m, Auto},
		{"Golden Sun: Dark Dawn", "nds", "Golden Sun Dark Dawn USA PROPER NDS", 0, 134 * m, Candidate},
		{"Golden Sun: Dark Dawn", "nds", "Golden Sun Dark Dawn", 9, 134 * m, Candidate},
		{"Golden Sun: Dark Dawn", "nds", "[NDS]_999,_Golden_Sun_Dark_Dawn,_Sonic_Colors (U)", 9, 276 * m, Candidate},
		{"Golden Sun", "gba", "(GBA Rom) Golden Sun 2 gba", 5, 16 * m, Candidate},
		{"Golden Sun", "gba", "Golden Sun OST GBA", 5, 16 * m, Candidate},
		{"Golden Sun", "gba", "Golden Sun NDS", 5, 16 * m, Reject},
		{"Golden Sun", "gba", "(GBA Rom) Golden Sun gba", 2, 8 * m, Auto},
		{"Golden Sun", "gba", "Golden Sun (Japan) GBA", 9, 8 * m, Candidate},
		{"Golden Sun", "gba", "Golden Sun GBA tiny", 9, 2 * 1024, Reject},
		{"Golden Sun", "gba", "Golden Sun GBA.exe", 9, 8 * m, Reject},
		{"Crash Bandicoot", "psx", "Crash Bandicoot (FULL PS1 Collection) (PSX) (incl. CTR n Bash)", 11, 700 * m, Candidate},
		{"Crash Bandicoot", "psx", "[PS4 PSX Classics] Crash Bandicoot [NTSC-U] [RUS/ENG] [1.00]", 4, 400 * m, Candidate},
		{"Crash Bandicoot", "psx", "Crash Bandicoot (USA) [PSX]", 4, 400 * m, Auto},
		{"Mario Kart DS", "nds", "Mario Kart DS (Europe) (En,Fr,De,Es,It) NDS", 6, 32 * m, Auto},
		{"Mario Kart DS", "nds", "Mario Kart 64 N64", 6, 32 * m, Reject},
		{"Final Fantasy VII", "psx", "Final Fantasy VII PSX --PSP", 9, 2000 * m, Candidate},
	}
	for _, c := range cases {
		got := verdict(t, want(c.title, c.platform), rel(c.release, c.seeders, c.size))
		if got.Verdict != c.want {
			t.Errorf("%s / %q: got %s (%v), want %s", c.title, c.release, got.Verdict, got.Reasons, c.want)
		}
	}
}

func TestRegionPreference(t *testing.T) {
	const m = int64(1) << 20
	w := want("Golden Sun", "gba")
	got := Rank([]Release{
		rel("Golden Sun (USA) GBA", 20, 8*m),
		rel("Golden Sun (France) GBA", 2, 8*m),
	}, w, false)
	if len(got) != 2 || got[0].Region != "fr" {
		t.Fatalf("French release should rank first: %+v", got)
	}
}

func TestAltNames(t *testing.T) {
	w := want("Oui-Oui: Une Journée au Pays des Jouets", "gba")
	w.AltNames = []string{"Noddy: A Day in Toyland"}
	got := verdict(t, w, rel("Noddy - A Day in Toyland (Europe) GBA", 3, 4<<20))
	if got.Verdict != Auto {
		t.Fatalf("alternative name should match exactly: %v", got.Reasons)
	}
}

func TestQueries(t *testing.T) {
	got := Queries("Pokémon Emerald Version", []string{"Pokemon Emerald", "Pokémon Émeraude", "ポケットモンスター エメラルド", "Pokémon Smeraldo"}, 4)
	want := []string{"Pokemon Emerald Version", "Pokemon Emerald", "Pokemon Emeraude", "Pokemon Smeraldo"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	got = Queries("Golden Sun: Dark Dawn", nil, 4)
	if strings.Join(got, "|") != "Golden Sun Dark Dawn|Golden Sun" {
		t.Fatalf("got %q", got)
	}
	if q := Queries("Ōkami", []string{"Okami"}, 4); len(q) != 1 || q[0] != "Okami" {
		t.Fatalf("got %q", q)
	}
}

func TestTrailingVersionIsNeutralButSubtitleIsNot(t *testing.T) {
	if v := verdict(t, want("Pokémon Emerald Version", "gba"), rel("Pokemon Emerald [USA] [GBA]", 7, 16<<20)).Verdict; v != Auto {
		t.Fatalf("Version should be neutral, got %s", v)
	}
	if v := verdict(t, want("Golden Sun: Dark Dawn", "nds"), rel("Golden Sun NDS", 7, 64<<20)).Verdict; v == Auto {
		t.Fatal("a subtitle must not be dropped for an exact match")
	}
}
