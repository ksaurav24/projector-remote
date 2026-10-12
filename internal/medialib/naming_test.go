package medialib

import (
	"slices"
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		path   string
		kind   Kind
		show   string
		year   int
		season int
		eps    []int
	}{
		// Scene releases.
		{"Breaking.Bad.S01E02.720p.BluRay.x264-DEMAND.mkv", KindEpisode, "Breaking Bad", 0, 1, []int{2}},
		{"TV Shows/Breaking Bad/Season 1/Breaking.Bad.S01E03.mkv", KindEpisode, "Breaking Bad", 0, 1, []int{3}},
		{"the.office.us.s02e01.hdtv.xvid.avi", KindEpisode, "The Office Us", 0, 2, []int{1}},
		{"Show/Show.S01E01E02.mkv", KindEpisode, "Show", 0, 1, []int{1, 2}},
		{"Show/Show.S01E01-E03.mkv", KindEpisode, "Show", 0, 1, []int{1, 2, 3}},
		{"Show/show s1 e5.mp4", KindEpisode, "Show", 0, 1, []int{5}},
		{"Lost/Lost - 1x04 - Walkabout.avi", KindEpisode, "Lost", 0, 1, []int{4}},
		{"Lost/Lost Season 2 Episode 7.mkv", KindEpisode, "Lost", 0, 2, []int{7}},
		// Season folders: spelled, abbreviated, misspelled, odd casing.
		{"Dark (2017)/Season 02/01 - Beginnings and Endings.mkv", KindEpisode, "Dark", 2017, 2, []int{1}},
		{"Dark/S03/Episode 4.mkv", KindEpisode, "Dark", 0, 3, []int{4}},
		{"Dark/SEASON1/E02.mkv", KindEpisode, "Dark", 0, 1, []int{2}},
		{"Dark/seson 2/03.mkv", KindEpisode, "Dark", 0, 2, []int{3}},
		{"Dark/Saeson 2/04.mkv", KindEpisode, "Dark", 0, 2, []int{4}},
		{"Doctor Who/Series 4/05 The Fires of Pompeii.mkv", KindEpisode, "Doctor Who", 0, 4, []int{5}},
		{"Doctor Who/Specials/01 The Next Doctor.mkv", KindEpisode, "Doctor Who", 0, 0, []int{1}},
		{"Friends/Season 1/101 - Pilot.mkv", KindEpisode, "Friends", 0, 1, []int{1}},
		// Season pack folder at the root.
		{"Breaking.Bad.S02.1080p.BluRay.x265/Breaking.Bad.S02E05.mkv", KindEpisode, "Breaking Bad", 0, 2, []int{5}},
		{"Breaking.Bad.S02.1080p.BluRay.x265/05.mkv", KindEpisode, "Breaking Bad", 0, 2, []int{5}},
		// Anime fansub naming, absolute numbers.
		{"Anime/Frieren/[SubsPlease] Sousou no Frieren - 05 (1080p) [A1B2C3D4].mkv", KindEpisode, "Frieren", 0, -1, []int{5}},
		{"[SubsPlease] Sousou no Frieren - 12 (1080p) [A1B2C3D4].mkv", KindEpisode, "Sousou no Frieren", 0, -1, []int{12}},
		{"One Piece/[Erai-raws] One Piece - 1085 [1080p].mkv", KindEpisode, "One Piece", 0, -1, []int{1085}},
		{"Naruto/Naruto Ep 12.mkv", KindEpisode, "Naruto", 0, -1, []int{12}},
		{"Naruto/Naruto_Episode_013v2.mkv", KindEpisode, "Naruto", 0, -1, []int{13}},
		// Movies.
		{"Movies/Inception (2010)/inception.2010.1080p.bluray.mkv", KindMovie, "Inception", 2010, -1, nil},
		{"The.Matrix.1999.2160p.UHD.BluRay.REMUX.HDR.mkv", KindMovie, "The Matrix", 1999, -1, nil},
		{"2001.A.Space.Odyssey.1968.1080p.mkv", KindMovie, "2001 A Space Odyssey", 1968, -1, nil},
		{"Blade Runner 2049 (2017).mp4", KindMovie, "Blade Runner 2049", 2017, -1, nil},
		{"Movies/Dune Part Two [2024] [2160p].mkv", KindMovie, "Dune Part Two", 2024, -1, nil},
		{"Movies/S.H.I.E.L.D.Origins.2015.mkv", KindMovie, "S.H.I.E.L.D Origins", 2015, -1, nil},
		{"Collection/Alien (1979).mkv", KindMovie, "Alien", 1979, -1, nil},
		// Not playable library items.
		{"Movies/Inception (2010)/Sample/sample.mkv", KindSkip, "", 0, 0, nil},
		{"Movies/Inception (2010)/inception-sample.mkv", KindSkip, "", 0, 0, nil},
		{"Show/Season 1/Extras/Making of.mkv", KindSkip, "", 0, 0, nil},
		{"Show/Season 1/notes.txt", KindSkip, "", 0, 0, nil},
		{".Trash/foo.mkv", KindSkip, "", 0, 0, nil},
	}
	for _, tc := range tests {
		got := ParsePath(tc.path)
		if got.Kind != tc.kind {
			t.Errorf("%s: kind %v, want %v (%+v)", tc.path, got.Kind, tc.kind, got)
			continue
		}
		if tc.kind == KindSkip {
			continue
		}
		if got.Show != tc.show || got.Year != tc.year || got.Season != tc.season || !slices.Equal(got.Episodes, tc.eps) {
			t.Errorf("%s: got show=%q year=%d season=%d eps=%v, want %q %d %d %v",
				tc.path, got.Show, got.Year, got.Season, got.Episodes, tc.show, tc.year, tc.season, tc.eps)
		}
	}
}

func TestEpisodeTitle(t *testing.T) {
	for path, want := range map[string]string{
		"Lost/Lost - 1x04 - Walkabout.avi":              "Walkabout",
		"Dark/Season 2/01 - Beginnings and Endings.mkv": "Beginnings and Endings",
		"Show/Show.S01E02.The.Second.One.720p.mkv":      "The Second One",
	} {
		if got := ParsePath(path).EpTitle; got != want {
			t.Errorf("%s: episode title %q, want %q", path, got, want)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	same := [][2]string{
		{"The Office", "office"},
		{"Grey's Anatomy", "Greys Anatomy"},
		{"Law & Order", "law and order"},
		{"Pokémon", "Pokemon"},
		{"BREAKING BAD", "Breaking.Bad"},
	}
	for _, p := range same {
		if normalizeKey(p[0]) != normalizeKey(p[1]) {
			t.Errorf("%q and %q should share a key: %q vs %q", p[0], p[1], normalizeKey(p[0]), normalizeKey(p[1]))
		}
	}
}
