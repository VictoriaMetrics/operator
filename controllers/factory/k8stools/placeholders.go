package k8stools

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RenderPlaceholders[T any](resource *T, placeholders map[string]string) (*T, error) {
	data, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource for filling placeholders: %w", err)
	}

	if len(placeholders) == 0 || len(data) == 0 {
		return resource, nil
	}

	strData := string(data)
	for placeholderName, value := range placeholders {
		placeholder := strings.ToUpper(fmt.Sprintf("%%%s%%", placeholderName))
		placeholder = strings.ReplaceAll(placeholder, ".", "_")
		placeholder = strings.ReplaceAll(placeholder, "-", "_")
		strData = strings.ReplaceAll(strData, placeholder, value)
	}

	var result *T
	err = json.Unmarshal([]byte(strData), &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource after filling placeholders: %w", err)
	}

	return result, nil
}
