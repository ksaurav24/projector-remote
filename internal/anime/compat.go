package anime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"projectorremote/internal/catalog"
)

// CompatHandler exposes the useful Anivexa route shapes on a self-hosted Go
// server. It intentionally does not recreate Anivexa's cache or proxy routes.
func CompatHandler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /{rest...}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /map/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := compatID(w, r.PathValue("id"))
		if !ok {
			return
		}
		m, err := service.Metadata.Get(r.Context(), id)
		if err != nil {
			compatError(w, http.StatusBadGateway, err)
			return
		}
		compatJSON(w, http.StatusOK, map[string]any{
			"anilistId": m.ID, "malId": nullableID(m.IDMal), "title": m.name(),
			"titles": m.titles(), "year": m.StartDate.Year, "format": m.Format, "status": m.Status,
		})
	})
	mux.HandleFunc("GET /episodes/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.PathValue("rest"), "/"), "/")
		if len(parts) == 0 || parts[len(parts)-1] == "" {
			compatError(w, http.StatusBadRequest, fmt.Errorf("invalid AniList ID"))
			return
		}
		id, ok := compatID(w, parts[len(parts)-1])
		if !ok {
			return
		}
		m, err := service.Metadata.Get(r.Context(), id)
		if err != nil {
			compatError(w, http.StatusBadGateway, err)
			return
		}
		unknown := []string{}
		allowed := make(map[string]bool, len(service.Providers))
		for _, p := range service.Providers {
			allowed[p.Name()] = true
		}
		for _, p := range parts[:len(parts)-1] {
			if !allowed[strings.ToLower(p)] {
				unknown = append(unknown, p)
			}
		}
		if len(parts) > 1 && len(unknown) == len(parts)-1 {
			compatJSON(w, http.StatusBadRequest, map[string]any{"error": "No valid providers specified", "unknown": unknown})
			return
		}
		selected := service.Providers
		if len(parts) > 1 {
			selected = nil
			wanted := make(map[string]bool)
			for _, name := range parts[:len(parts)-1] {
				if allowed[strings.ToLower(name)] {
					wanted[strings.ToLower(name)] = true
				}
			}
			for _, p := range service.Providers {
				if wanted[strings.ToLower(p.Name())] {
					selected = append(selected, p)
				}
			}
		}
		page := episodePage(m, "sub", 1)
		items := make([]map[string]any, 0, len(page.Items))
		for _, e := range page.Items {
			items = append(items, map[string]any{"episode": e.Num, "title": e.Title, "audio": "sub"})
		}
		compatJSON(w, http.StatusOK, map[string]any{
			"anilistId": m.ID,
			"episodes":  map[string]any{"sub": items},
			"providers": selectedProviderNames(selected), "_unknownProviders": unknown,
			"note": "episode numbers are AniList metadata; provider-specific availability is resolved by /watch",
		})
	})
	mux.HandleFunc("GET /watch/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.PathValue("rest"), "/"), "/")
		if len(parts) != 4 {
			compatError(w, http.StatusBadRequest, fmt.Errorf("invalid watch route"))
			return
		}
		provider, idText, audio, epText := strings.ToLower(parts[0]), parts[1], parts[2], parts[3]
		prefix := provider + "-"
		if strings.HasPrefix(epText, prefix) {
			epText = strings.TrimPrefix(epText, prefix)
		}
		if audio != "sub" && audio != "dub" {
			compatError(w, http.StatusBadRequest, fmt.Errorf("audio must be sub or dub"))
			return
		}
		if !providerKnown(service.Providers, provider) {
			compatError(w, http.StatusBadRequest, fmt.Errorf("unknown anime provider"))
			return
		}
		id, ok := compatID(w, idText)
		if !ok {
			return
		}
		ep, err := strconv.Atoi(epText)
		if err != nil || ep < 1 {
			compatError(w, http.StatusBadRequest, fmt.Errorf("invalid episode"))
			return
		}
		stream, err := service.ResolveProvider(r.Context(), provider, fmt.Sprintf("%d:%s:%d", id, audio, ep))
		if err != nil {
			compatError(w, compatStatus(err), err)
			return
		}
		compatJSON(w, http.StatusOK, map[string]any{
			"anilistId": id, "episode": ep, "audio": audio,
			"streams":   []map[string]any{{"url": stream.URL, "type": compatStreamType(stream.MimeType), "referer": stream.Referer, "headers": stream.Headers}},
			"subtitles": stream.Subtitles,
		})
	})
	mux.HandleFunc("GET /stream/reanime/{id}/{audio}/{ep}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := compatID(w, r.PathValue("id"))
		if !ok {
			return
		}
		audio, ep := r.PathValue("audio"), r.PathValue("ep")
		stream, err := service.ResolveProvider(r.Context(), "reanime", fmt.Sprintf("%d:%s:%s", id, audio, ep))
		if err != nil {
			compatError(w, compatStatus(err), err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Redirect(w, r, stream.URL, http.StatusFound)
	})
	return mux
}

func compatID(w http.ResponseWriter, raw string) (int, bool) {
	id, err := strconv.Atoi(raw)
	if err != nil || id < 1 {
		compatError(w, http.StatusBadRequest, fmt.Errorf("invalid AniList ID"))
		return 0, false
	}
	return id, true
}
func compatStatus(err error) int {
	if errors.Is(err, catalog.ErrInvalidArgument) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}
func compatError(w http.ResponseWriter, status int, err error) {
	compatJSON(w, status, map[string]string{"error": err.Error()})
}
func compatJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func nullableID(id int) any {
	if id < 1 {
		return nil
	}
	return id
}
func compatStreamType(mime string) string {
	if mime == "application/x-mpegURL" {
		return "hls"
	}
	return "mp4"
}
func providerKnown(providers []Provider, name string) bool {
	for _, p := range providers {
		if p.Name() == name {
			return true
		}
	}
	return false
}
func selectedProviderNames(providers []Provider) []string {
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.Name())
	}
	return out
}
