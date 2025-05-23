package v1

import "fmt"

const (
	healthPath = "/health"
	metricPath = "/metrics"
)

func prefixedName(name, prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}
