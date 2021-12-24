package k8stools

// MergeAnnotations adds well known annotations to the current map from prev
// It's needed for kubectl restart at least.
func MergeAnnotations(prev, current map[string]string) map[string]string {
	if value, ok := prev["kubectl.kubernetes.io/restartedAt"]; ok {
		if current == nil {
			current = make(map[string]string)
		}
		current["kubectl.kubernetes.io/restartedAt"] = value
	}
	return current
}
