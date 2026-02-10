package vmdistributed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func fetchMetricValues(ctx context.Context, httpClient *http.Client, url, metricName, dimension string) (map[string]float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for VMAgent at %s: %w", url, err)
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics from VMAgent at %s: %w", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read VMAgent metrics at %s: %w", url, err)
	}
	s := string(data)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response status=%d, body=%q while requesting VMAgent metrics at %s", resp.StatusCode, s, url)
	}
	values := make(map[string]float64)
	if err := unmarshalMetric(values, s, metricName, dimension); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metric=%s values at %s: %w", metricName, url, err)
	}
	return values, nil
}

func unmarshalMetric(values map[string]float64, s, name, dimension string) error {
	for {
		idx := strings.Index(s, name)
		if idx == -1 {
			break
		}
		s = s[idx+len(name):]
		s = skipLeadingWhitespace(s)
		n := strings.IndexByte(s, '{')
		var dimValue string
		if n >= 0 {
			// Tags found. Parse them.
			s = s[n+1:]
			var err error
			var tags map[string]string
			s, tags, err = unmarshalTags(s)
			if err != nil {
				return fmt.Errorf("cannot unmarshal tags: %w", err)
			}
			dimValue = tags[dimension]
		}
		s = skipLeadingWhitespace(s)
		if len(s) == 0 {
			return fmt.Errorf("value cannot be empty")
		}
		valStr := s
		n = strings.IndexAny(s, " \t\r\n")
		if n >= 0 {
			valStr = valStr[:n]
		}
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return fmt.Errorf("cannot parse value %q: %w", s, err)
		}
		s = s[len(valStr):]
		if len(dimValue) > 0 {
			values[dimValue] = v
		}
	}
	return nil
}

func unmarshalTags(s string) (string, map[string]string, error) {
	tags := make(map[string]string)
	var err error
	for {
		s = skipLeadingWhitespace(s)
		n := strings.IndexByte(s, '"')
		if n < 0 {
			// end of tags
			if len(s) > 0 && s[0] == '}' {
				return s[1:], tags, nil
			}
			return s, tags, fmt.Errorf("missing value for tag %q", s)
		}
		// Determine if this is a value or quoted label
		possibleKey := skipTrailingWhitespace(s[:n])
		possibleKeyLen := len(possibleKey)
		key := ""
		if possibleKeyLen == 0 {
			// Parse quoted label - {"label"="value"} or {"metric"}
			key, s, err = unmarshalQuotedString(s)
			if err != nil {
				return s, tags, err
			}
			s = skipLeadingWhitespace(s)
			if len(s) > 0 {
				if s[0] == ',' || s[0] == '}' {
					if len(s) > 1 && s[0] == ',' {
						s = s[1:]
					}
					continue
				} else if s[0] != '=' {
					// We are a quoted label that isn't preceded by a comma or at the end
					// of the tags so we must have a value
					return s, tags, fmt.Errorf("missing value for quoted tag %q", key)
				}
				s = skipLeadingWhitespace(s[1:])
			}
			// Fall through to parsing value
		} else {
			c := possibleKey[len(possibleKey)-1]
			// unquoted label {label="value"}
			if c == '=' {
				// Parse unquoted label
				key = skipLeadingWhitespace(s[:possibleKeyLen-1])
				key = skipTrailingWhitespace(key)
				s = skipLeadingWhitespace(s[possibleKeyLen:])
			} else {
				// unquoted tag without a value
				return s, tags, fmt.Errorf("missing value for unquoted tag %q", s)
			}
		}
		// Parse value
		var value string
		value, s, err = unmarshalQuotedString(s)
		if err != nil {
			return s, tags, err
		}

		if len(key) > 0 {
			// Allow empty values (len(value)==0) - see https://github.com/VictoriaMetrics/VictoriaMetrics/issues/453
			tags[key] = value
		}
		s = skipLeadingWhitespace(s)
		if len(s) > 0 && s[0] == '}' {
			// End of tags found.
			return s[1:], tags, nil
		}
		if len(s) == 0 || s[0] != ',' {
			return s, tags, fmt.Errorf("missing comma after tag %s=%q", key, value)
		}
		s = s[1:]
	}
}

func skipTrailingWhitespace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func skipLeadingWhitespace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

func unmarshalQuotedString(s string) (string, string, error) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, fmt.Errorf("missing starting double quote in string: %q", s)
	}
	n := findClosingQuote(s)
	if n == -1 {
		return "", s, fmt.Errorf("missing closing double quote in string: %q", s)
	}
	return unescapeValue(s[1:n]), s[n+1:], nil
}

func unescapeValue(s string) string {
	n := strings.IndexByte(s, '\\')
	if n < 0 {
		// Fast path - nothing to unescape
		return s
	}
	b := make([]byte, 0, len(s))
	for {
		b = append(b, s[:n]...)
		s = s[n+1:]
		if len(s) == 0 {
			b = append(b, '\\')
			break
		}
		// label_value can be any sequence of UTF-8 characters, but the backslash (\), double-quote ("),
		// and line feed (\n) characters have to be escaped as \\, \", and \n, respectively.
		// See https://github.com/prometheus/docs/blob/e39897e4ee6e67d49d47204a34d120e3314e82f9/docs/instrumenting/exposition_formats.md
		switch s[0] {
		case '\\':
			b = append(b, '\\')
		case '"':
			b = append(b, '"')
		case 'n':
			b = append(b, '\n')
		default:
			b = append(b, '\\', s[0])
		}
		s = s[1:]
		n = strings.IndexByte(s, '\\')
		if n < 0 {
			b = append(b, s...)
			break
		}
	}
	return string(b)
}

func findClosingQuote(s string) int {
	if len(s) == 0 || s[0] != '"' {
		return -1
	}
	off := 1
	s = s[1:]
	for {
		n := strings.IndexByte(s, '"')
		if n < 0 {
			return -1
		}
		if prevBackslashesCount(s[:n])%2 == 0 {
			return off + n
		}
		off += n + 1
		s = s[n+1:]
	}
}

func prevBackslashesCount(s string) int {
	n := 0
	for len(s) > 0 && s[len(s)-1] == '\\' {
		n++
		s = s[:len(s)-1]
	}
	return n
}

func mergeSpecs[T any](a, b *T, name string) (*T, error) {
	merged, err := k8stools.RenderPlaceholders(a, map[string]string{
		vmv1alpha1.ZonePlaceholder: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render spec: %w", err)
	}

	// Apply cluster-specific override if it exist
	if err := build.MergeDeep(merged, b); err != nil {
		return nil, fmt.Errorf("failed to merge spec: %w", err)
	}
	return merged, nil
}
