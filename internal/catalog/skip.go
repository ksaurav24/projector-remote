package catalog

import "math"

// Skip ranges use milliseconds, like player positions. Source identifies the
// timestamp origin without exposing provider routing or media URLs to clients.
type SkipRange struct {
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Source string `json:"source,omitempty"`
}

type SkipRanges struct {
	Intro *SkipRange `json:"intro,omitempty"`
	Outro *SkipRange `json:"outro,omitempty"`
}

func SecondsRange(start, end float64, source string) *SkipRange {
	if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end <= start || end > 24*60*60 {
		return nil
	}
	r := &SkipRange{Start: int64(math.Round(start * 1000)), End: int64(math.Round(end * 1000)), Source: source}
	if r.End <= r.Start {
		return nil
	}
	return r
}

func (r SkipRanges) ForDuration(dur int64) SkipRanges {
	valid := func(v *SkipRange) *SkipRange {
		if v == nil || v.Start < 0 || v.End <= v.Start || (dur > 0 && v.End > dur) {
			return nil
		}
		return v
	}
	return SkipRanges{Intro: valid(r.Intro), Outro: valid(r.Outro)}
}

func (r SkipRanges) Fill(other SkipRanges) SkipRanges {
	if r.Intro == nil {
		r.Intro = other.Intro
	}
	if r.Outro == nil {
		r.Outro = other.Outro
	}
	return r
}
