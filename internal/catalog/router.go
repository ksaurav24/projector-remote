package catalog

import "context"

// Source is the normalized catalogue and playback contract. Provider-specific
// identifiers, authentication and extraction never cross this boundary.
type Source interface {
	Home(context.Context, string) (*Home, error)
	Search(context.Context, string, string) ([]SearchItem, error)
	Title(context.Context, string, string) (*Title, error)
	Season(context.Context, string, string, int) (*EpisodePage, error)
	Resolve(context.Context, string, string) (*Stream, error)
}

type LoginSource interface {
	Source
	LoginImage(context.Context) (string, error)
	Login(context.Context, string) (string, error)
}

type Router struct {
	Default LoginSource
	Anime   Source
	// Local is the laptop media library; nil when not configured.
	Local AvailableSource
}

// AvailableSource is a source that may come and go.
type AvailableSource interface {
	Source
	Available() bool
}

func (r *Router) source(ott string) Source {
	switch ott {
	case "anime":
		return r.Anime
	case "local":
		if r.Local == nil {
			return offline{}
		}
		return r.Local
	}
	return r.Default
}

// Available reports whether a catalogue can be browsed right now.
func (r *Router) Available(ott string) bool {
	if ott == "local" {
		return r.Local != nil && r.Local.Available()
	}
	return ValidOTT(ott)
}

type offline struct{}

func (offline) Home(context.Context, string) (*Home, error)                  { return nil, ErrOffline }
func (offline) Search(context.Context, string, string) ([]SearchItem, error) { return nil, ErrOffline }
func (offline) Title(context.Context, string, string) (*Title, error)        { return nil, ErrOffline }
func (offline) Season(context.Context, string, string, int) (*EpisodePage, error) {
	return nil, ErrOffline
}
func (offline) Resolve(context.Context, string, string) (*Stream, error) { return nil, ErrOffline }
func (r *Router) Home(c context.Context, o string) (*Home, error)        { return r.source(o).Home(c, o) }
func (r *Router) Search(c context.Context, o, q string) ([]SearchItem, error) {
	return r.source(o).Search(c, o, q)
}
func (r *Router) Title(c context.Context, o, id string) (*Title, error) {
	return r.source(o).Title(c, o, id)
}
func (r *Router) Season(c context.Context, o, id string, p int) (*EpisodePage, error) {
	return r.source(o).Season(c, o, id, p)
}
func (r *Router) Resolve(c context.Context, o, id string) (*Stream, error) {
	return r.source(o).Resolve(c, o, id)
}

func (r *Router) SkipTimes(c context.Context, o, id string, stream *Stream, duration int64) SkipRanges {
	if source, ok := r.source(o).(interface {
		SkipTimes(context.Context, string, string, *Stream, int64) SkipRanges
	}); ok {
		return source.SkipTimes(c, o, id, stream, duration)
	}
	return stream.SkipRanges.ForDuration(duration)
}
func (r *Router) LoginImage(c context.Context) (string, error)        { return r.Default.LoginImage(c) }
func (r *Router) Login(c context.Context, otp string) (string, error) { return r.Default.Login(c, otp) }

// Recover skips the source whose stream failed, without changing content IDs.
func (r *Router) Recover(c context.Context, o, id string, failed *Stream) (*Stream, error) {
	if source, ok := r.source(o).(interface {
		Recover(context.Context, string, string, *Stream) (*Stream, error)
	}); ok {
		return source.Recover(c, o, id, failed)
	}
	return r.Resolve(c, o, id)
}
