package podutil

import (
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func FuzzFetchMetricsValues(f *testing.F) {
	for _, seed := range []struct {
		body, name, dim, name2, dim2 string
		ok                           bool
	}{
		{"", "m", "", "", "", true},
		{"m 1\n", "m", "", "", "", true},
		{"m 1\n", "m", "", "m", "d", true},
		{"# HELP m h\n# TYPE m gauge\nm{path=\"a\"} 24\n", "m", "path", "", "", true},
		{"m_total{path=\"x\"} 9\nm{path=\"y\"} 1e3\n", "m", "path", "m_total", "path", true},
		{"m{\"path\"=\"a\",b=\"c\"} 1\n", "m", "path", "", "", true},
		{"m{\"m\",path=\"a\"} NaN\n", "m", "path", "", "", true},
		{"m{path=\"a\\\"b\\\\c\\nd\\x\"} +Inf 1700000000\n", "m", "path", "", "", true},
		{"m{path=\"a\" 1\n", "m", "path", "", "", true},
		{"m{path} 1\n", "m", "path", "", "", true},
		{"m{path=a} 1\n", "m", "path", "", "", true},
		{"m{path=\"a\"b=\"c\"} 1\n", "m", "path", "", "", true},
		{"m{\"path\" \"a\"} 1\n", "m", "path", "", "", true},
		{"m{path=\"unterminated} 1\n", "m", "path", "", "", true},
		{"m{path=\"a\"}\n", "m", "path", "", "", true},
		{"m abc\n", "m", "", "", "", true},
		{"m\t1\r\n", "m", "", "", "", true},
		{"m{path=\"\"} 1\n", "m", "path", "", "", true},
		{"boom", "m", "", "", "", false},
	} {
		f.Add(seed.body, seed.name, seed.dim, seed.name2, seed.dim2, seed.ok)
	}
	f.Fuzz(func(t *testing.T, body, name, dim, name2, dim2 string, ok bool) {
		status := http.StatusOK
		if !ok {
			status = http.StatusInternalServerError
		}
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       readCloser{strings.NewReader(body)},
				Request:    r,
			}, nil
		})}
		queries := []MetricQuery{{Name: name, Dimension: dim}}
		if name2 != "" {
			queries = append(queries, MetricQuery{Name: name2, Dimension: dim2})
		}
		_, _ = FetchMetricsValues(t.Context(), client, "http://localhost/metrics", queries)
	})
}

type readCloser struct{ *strings.Reader }

func (readCloser) Close() error { return nil }
