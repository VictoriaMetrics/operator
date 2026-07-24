package podutil

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxMetricLineBytes bounds a single scanned line of a /metrics response. It exists only to
// cap pathological input (e.g. a single unbounded line) - legitimately large /metrics
// responses are handled fine since they're never buffered in full, only scanned line by line.
const maxMetricLineBytes = 1 << 20 // 1MiB

// maxErrBodyBytes bounds how much of a non-200 response body is read for the error message.
const maxErrBodyBytes = 4 << 10 // 4KiB

// MetricQuery selects a metric by Name and, optionally, a label (Dimension) whose value is
// used to key each sample of that metric. If Dimension is empty, every sample of the metric
// is kept regardless of its labels, keyed by a synthetic index.
type MetricQuery struct {
	Name      string
	Dimension string
}

// FetchMetricsValues issues a single HTTP GET to url and extracts every sample matching any
// of the given queries, returning metric name -> (dimension value -> sample value).
func FetchMetricsValues(ctx context.Context, httpClient *http.Client, url string, queries []MetricQuery) (map[string]map[string]float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", url, err)
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics from %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
		return nil, fmt.Errorf("unexpected response status=%d, body=%q while requesting metrics at %s", resp.StatusCode, body, url)
	}
	dimensions := make(map[string]string, len(queries))
	for _, q := range queries {
		if previous, exists := dimensions[q.Name]; exists && previous != q.Dimension {
			return nil, fmt.Errorf("metric %q requested with conflicting dimensions %q and %q", q.Name, previous, q.Dimension)
		}
		dimensions[q.Name] = q.Dimension
	}
	values := make(map[string]map[string]float64, len(queries))
	if err := unmarshalMetrics(values, resp.Body, dimensions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics values at %s: %w", url, err)
	}
	return values, nil
}

// unmarshalMetrics scans r line by line rather than buffering it in full, so a large
// /metrics response never inflates controller memory - only one line is held at a time.
func unmarshalMetrics(values map[string]map[string]float64, r io.Reader, dimensions map[string]string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxMetricLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		n := strings.IndexAny(line, "{ \t\r")
		metricName := line
		if n >= 0 {
			metricName = line[:n]
		}
		dimension, ok := dimensions[metricName]
		if !ok {
			continue
		}

		line = line[len(metricName):]
		line = skipLeadingWhitespace(line)
		var dimValue string
		if len(line) > 0 && line[0] == '{' {
			line = line[1:]
			var err error
			var tags map[string]string
			line, tags, err = unmarshalTags(line)
			if err != nil {
				return fmt.Errorf("cannot unmarshal tags: %w", err)
			}
			if dimension != "" {
				dimValue = tags[dimension]
			}
		}
		line = skipLeadingWhitespace(line)
		if len(line) == 0 {
			return fmt.Errorf("value cannot be empty")
		}
		valStr := line
		n = strings.IndexAny(line, " \t\r\n")
		if n >= 0 {
			valStr = valStr[:n]
		}
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return fmt.Errorf("cannot parse value %q: %w", line, err)
		}
		metricValues := values[metricName]
		if metricValues == nil {
			metricValues = make(map[string]float64)
			values[metricName] = metricValues
		}
		if len(dimValue) > 0 {
			metricValues[dimValue] = v
		} else if dimension == "" {
			metricValues[strconv.Itoa(len(metricValues))] = v
		}
	}
	return scanner.Err()
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
