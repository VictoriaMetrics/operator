package k8stools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderPlaceholders replaces placeholders at resource with given values
// placeholder must be in %NAME% format
// resource must be reference to json serializable struct
func RenderPlaceholders[T any](resource *T, placeholders map[string]string) (*T, error) {
	if resource == nil || len(placeholders) == 0 {
		return resource, nil
	}

	data, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource for filling placeholders: %w", err)
	}

	strData := string(data)
	for p, value := range placeholders {
		if !strings.HasPrefix(p, "%") || !strings.HasSuffix(p, "%") {
			return nil, fmt.Errorf("incorrect placeholder name format: '%v', placeholder must be in '%%NAME%%' format", p)
		}
		strData = strings.ReplaceAll(strData, p, value)
	}

	var result *T
	err = json.Unmarshal([]byte(strData), &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource after filling placeholders: %w", err)
	}

	return result, nil
}
