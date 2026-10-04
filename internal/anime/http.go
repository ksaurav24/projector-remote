package anime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36"

var defaultHTTP = &http.Client{Timeout: 15 * time.Second}

type upstreamStatusError int

func (s upstreamStatusError) Error() string { return fmt.Sprintf("upstream HTTP %d", s) }

func request(ctx context.Context, client *http.Client, method, target string, body any, headers map[string]string) ([]byte, error) {
	b, err := requestOnce(ctx, client, method, target, body, headers)
	if err == nil || method != "GET" || ctx.Err() != nil {
		return b, err
	}
	if status, ok := err.(upstreamStatusError); ok && status < 500 {
		return b, err
	}
	// One bounded retry absorbs transient origin/CDN failures, without requiring
	// the user to press Play again or retrying permanent 4xx responses.
	return requestOnce(ctx, client, method, target, body, headers)
}
func requestOnce(ctx context.Context, client *http.Client, method, target string, body any, headers map[string]string) ([]byte, error) {
	// Leave time for a second request inside the overall provider budget;
	// otherwise one stalled origin consumes both the attempt and its retry.
	if method == "GET" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, upstreamStatusError(res.StatusCode)
	}
	const limit = 8 << 20
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if len(b) > limit {
		return nil, fmt.Errorf("upstream response too large")
	}
	return b, err
}
func getJSON(ctx context.Context, client *http.Client, target string, out any) error {
	b, e := request(ctx, client, "GET", target, nil, nil)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func endpoint(base, path string) string { return strings.TrimRight(base, "/") + path }
