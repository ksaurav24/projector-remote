package history

import "fmt"

type SkipSettings struct {
	Intro string `json:"intro"`
	Outro string `json:"outro"`
}

func (p SkipSettings) Defaults() SkipSettings {
	valid := func(mode string) string {
		if mode == "auto" || mode == "off" {
			return mode
		}
		return "manual"
	}
	return SkipSettings{Intro: valid(p.Intro), Outro: valid(p.Outro)}
}

func (s *Store) SkipSettings() SkipSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.AnimeSkip.Defaults()
}

func (s *Store) SetSkipMode(kind, mode string) error {
	if (kind != "intro" && kind != "outro") || (mode != "manual" && mode != "auto" && mode != "off") {
		return fmt.Errorf("choose Manual, Automatic, or Off for intro or outro")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.AnimeSkip
	next := previous.Defaults()
	if kind == "intro" {
		next.Intro = mode
	} else {
		next.Outro = mode
	}
	s.data.AnimeSkip = next
	if err := s.persist(); err != nil {
		s.data.AnimeSkip = previous
		return fmt.Errorf("couldn't save anime skip settings: %w", err)
	}
	return nil
}
