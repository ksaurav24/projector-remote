package playback

import (
	"context"
	"fmt"
	"slices"

	"projectorremote/internal/catalog"
)

// Catalog is the provider-neutral subset needed by playback.
type Catalog interface {
	Title(ctx context.Context, ott, id string) (*catalog.Title, error)
	Season(ctx context.Context, ott, seasonID string, page int) (*catalog.EpisodePage, error)
	Resolve(ctx context.Context, ott, id string) (*catalog.Stream, error)
}

// seriesCursor is one episode's place in its series: the season it's
// in, that season's episodes loaded so far (NetMirror pages them ten at
// a time), and whether more pages follow.
type seriesCursor struct {
	seasons  []catalog.Season
	seasonIx int
	edition  string
	episodes []catalog.Episode
	nextPage *int
	epIx     int
}

// locate finds episodeID in seasonID, loading the season's pages until
// it turns up. An empty seasonID means the first season; an empty
// episodeID means that season's first episode.
func locate(ctx context.Context, cat Catalog, ott string, t *catalog.Title, seasonID, episodeID string) (*seriesCursor, error) {
	if len(t.Seasons) == 0 {
		return nil, fmt.Errorf("playback: %s has no seasons", t.ID)
	}
	if seasonID == "" {
		seasonID = t.Seasons[0].ID
		if ott == "anime" && t.SeasonID != "" {
			seasonID = t.SeasonID
		}
	}
	ix := slices.IndexFunc(t.Seasons, func(s catalog.Season) bool {
		return s.ID == seasonID || slices.ContainsFunc(s.Editions, func(e catalog.Edition) bool { return e.ID == seasonID })
	})
	if ix < 0 {
		return nil, fmt.Errorf("playback: season %s not in %s", seasonID, t.ID)
	}
	c := &seriesCursor{seasons: t.Seasons, seasonIx: ix}
	for _, e := range t.Seasons[ix].Editions {
		if e.ID == seasonID {
			c.edition = e.Code
		}
	}

	page := t.Episodes
	if t.SeasonID != seasonID || page == nil {
		var err error
		if page, err = cat.Season(ctx, ott, seasonID, 1); err != nil {
			return nil, err
		}
	}
	c.addPage(page)

	for {
		for i, e := range c.episodes {
			if episodeID == "" || e.ID == episodeID {
				c.epIx = i
				return c, nil
			}
		}
		if c.nextPage == nil {
			return nil, fmt.Errorf("playback: episode %q not in season %s", episodeID, seasonID)
		}
		if err := c.loadNextPage(ctx, cat, ott); err != nil {
			return nil, err
		}
	}
}

func (c *seriesCursor) addPage(p *catalog.EpisodePage) {
	c.episodes = append(c.episodes, p.Items...)
	c.nextPage = p.NextPage
	// Guard against a page that promises more but brings nothing, which
	// would otherwise loop forever.
	if len(p.Items) == 0 {
		c.nextPage = nil
	}
}

func (c *seriesCursor) loadNextPage(ctx context.Context, cat Catalog, ott string) error {
	p, err := cat.Season(ctx, ott, c.season().ID, *c.nextPage)
	if err != nil {
		return err
	}
	c.addPage(p)
	return nil
}

func (c *seriesCursor) withEdition(s catalog.Season) catalog.Season {
	for _, e := range s.Editions {
		if e.Code == c.edition {
			s.ID = e.ID
			break
		}
	}
	return s
}
func (c *seriesCursor) season() catalog.Season   { return c.withEdition(c.seasons[c.seasonIx]) }
func (c *seriesCursor) current() catalog.Episode { return c.episodes[c.epIx] }
func epLabel(season, episode int) string         { return fmt.Sprintf("S%d E%d", season, episode) }
func (c *seriesCursor) label() string            { return epLabel(c.season().Number, c.current().Num) }

func episodeLabel(ott string, season catalog.Season, ep catalog.Episode) string {
	if ott == "anime" {
		return fmt.Sprintf("%s · E%d", season.Label, ep.Num)
	}
	return epLabel(season.Number, ep.Num)
}

// next returns the episode after the current one: later on this page,
// on the season's next page, or first in the next season. ok is false
// after the series finale.
func (c *seriesCursor) next(ctx context.Context, cat Catalog, ott string) (season catalog.Season, ep catalog.Episode, ok bool, err error) {
	if c.epIx+1 >= len(c.episodes) && c.nextPage != nil {
		if err := c.loadNextPage(ctx, cat, ott); err != nil {
			return season, ep, false, err
		}
	}
	if c.epIx+1 < len(c.episodes) {
		return c.season(), c.episodes[c.epIx+1], true, nil
	}
	for ix := c.seasonIx + 1; ix < len(c.seasons); ix++ {
		nextSeason := c.withEdition(c.seasons[ix])
		p, err := cat.Season(ctx, ott, nextSeason.ID, 1)
		if err != nil {
			return season, ep, false, err
		}
		if len(p.Items) > 0 {
			return nextSeason, p.Items[0], true, nil
		}
	}
	return season, ep, false, nil
}
