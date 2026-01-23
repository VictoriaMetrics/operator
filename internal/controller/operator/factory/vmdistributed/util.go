package vmdistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
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

// mergeDeep merges an override object into a base one.
// Fields present in the override will overwrite corresponding fields in the base.
func mergeDeep[T comparable](base, override T) (bool, error) {
	var zero T
	if override == zero {
		return false, nil
	}

	baseJSON, err := json.Marshal(base)
	if err != nil {
		return false, fmt.Errorf("failed to marshal base spec: %w", err)
	}
	overrideJSON, err := json.Marshal(override)
	if err != nil {
		return false, fmt.Errorf("failed to marshal override spec: %w", err)
	}

	var baseMap map[string]any
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return false, fmt.Errorf("failed to unmarshal base spec to map: %w", err)
	}
	var overrideMap map[string]any
	if err := json.Unmarshal(overrideJSON, &overrideMap); err != nil {
		return false, fmt.Errorf("failed to unmarshal override spec to map: %w", err)
	}

	// Perform a deep merge: fields from overrideMap recursively overwrite corresponding fields in baseMap.
	// If an override value is explicitly nil, it signifies the removal or nullification of that field.
	updated := mergeMapsRecursive(baseMap, overrideMap)
	mergedSpecJSON, err := json.Marshal(baseMap)
	if err != nil {
		return false, fmt.Errorf("failed to marshal merged spec map: %w", err)
	}

	if err := json.Unmarshal(mergedSpecJSON, base); err != nil {
		return false, fmt.Errorf("failed to unmarshal merged spec JSON: %w", err)
	}

	return updated, nil
}

// mergeMapsRecursive deeply merges overrideMap into baseMap.
// It handles nested maps (which correspond to nested structs after JSON unmarshal).
// Values from overrideMap overwrite values in baseMap.
// It returns a boolean indicating if the baseMap was modified.
func mergeMapsRecursive(baseMap, overrideMap map[string]any) bool {
	var modified bool
	if len(overrideMap) == 0 {
		return modified
	}
	for key, overrideValue := range overrideMap {
		if baseVal, ok := baseMap[key]; ok {
			if baseMapNested, isBaseMap := baseVal.(map[string]any); isBaseMap {
				if overrideMapNested, isOverrideMap := overrideValue.(map[string]any); isOverrideMap {
					// Both are nested maps, recurse
					if mergeMapsRecursive(baseMapNested, overrideMapNested) {
						modified = true
					}
					continue
				}
			}
		}

		// For all other cases (scalar values, or when types for nested maps don't match),
		// override the baseMap value. This handles explicit zero values and ensures
		// overrides take precedence.
		// We assign first, then check if it was a modification.
		oldValue, exists := baseMap[key]
		baseMap[key] = overrideValue // Force the overwrite for this key
		if !exists || !reflect.DeepEqual(oldValue, overrideValue) {
			modified = true
		}
	}
	return modified
}
