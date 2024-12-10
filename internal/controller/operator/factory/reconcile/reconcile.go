package reconcile

import (
	"time"
)

var (
	podWaitReadyIntervalCheck = 50 * time.Millisecond
	appWaitReadyDeadline      = 5 * time.Second
	podWaitReadyTimeout       = 5 * time.Second
)

// InitFromConfig sets package configuration from config
func InitDeadlines(intervalCheck, appWaitDeadline, podReadyDeadline time.Duration) {
	podWaitReadyIntervalCheck = intervalCheck
	appWaitReadyDeadline = appWaitDeadline
	podWaitReadyTimeout = podReadyDeadline
}

// mergeAnnotations performs 3-way merge for annotations
// it deletes only annotations managed by operator CRDs
// 3-rd party kubernetes annotations must be preserved
func mergeAnnotations(currentA, newA, prevA map[string]string) map[string]string {
	dst := make(map[string]string)
	var deleted map[string]struct{}

	for k := range prevA {
		if _, ok := newA[k]; !ok {
			if deleted == nil {
				deleted = make(map[string]struct{})
			}
			deleted[k] = struct{}{}
		}
	}

	for k, v := range currentA {
		if _, ok := deleted[k]; ok {
			continue
		}
		dst[k] = v
	}
	for k, v := range newA {
		dst[k] = v
	}
	return dst
}

// isAnnotationsEqual properly track changes at object annotations
// it preserves 3rd party annotations
func isAnnotationsEqual(currentA, newA, prevA map[string]string) bool {
	for k, v := range newA {
		cv, ok := currentA[k]
		if !ok {
			return false
		}
		if v != cv {
			return false
		}
	}
	for k := range prevA {
		_, nok := newA[k]
		_, cok := currentA[k]
		// case for annotations delete
		if nok != cok {
			return false
		}
	}
	return true
}
