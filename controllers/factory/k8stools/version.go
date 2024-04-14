package k8stools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// ServerMajorVersion defines major number for current kubernetes API server version
	ServerMajorVersion uint64
	// ServerMinorVersion defines minor number for current kubernetes API server version
	ServerMinorVersion uint64
)

// SetKubernetesVersionWithDefaults parses kubernetes version response with given default versions
func SetKubernetesVersionWithDefaults(vi *version.Info, defaultMinor, defaultMajor uint64) error {
	var warnMessage string
	minor := strings.Trim(vi.Minor, "+")
	v, err := strconv.ParseUint(minor, 10, 64)
	if err != nil {
		v = defaultMinor
		warnMessage = fmt.Sprintf("cannot parse minor kubernetes version response: %s, err: %s, using default: %d\n", vi.Minor, err, defaultMinor)
	}
	ServerMinorVersion = v
	major := strings.Trim(vi.Major, "+")
	v, err = strconv.ParseUint(major, 10, 64)
	if err != nil {
		v = defaultMajor
		warnMessage += fmt.Sprintf("cannot parse major kubernetes version response: %s, err: %s, using default: %d\n", vi.Major, err, defaultMajor)
	}
	ServerMajorVersion = v
	if len(warnMessage) > 0 {
		return fmt.Errorf(warnMessage)
	}
	return nil
}

// IsPSPSupported check if PodSecurityPolicy is supported by kubernetes API server
// https://kubernetes.io/docs/reference/using-api/deprecation-guide/#psp-v125
func IsPSPSupported() bool {
	if ServerMajorVersion == 1 && ServerMinorVersion <= 24 {
		return true
	}
	return false
}

// IsPDBV1APISupported check if new v1 API is supported by kubernetes API server
// deprecated since 1.21
// https://kubernetes.io/docs/reference/using-api/deprecation-guide/#poddisruptionbudget-v125
func IsPDBV1APISupported() bool {
	if ServerMajorVersion == 1 && ServerMinorVersion >= 21 {
		return true
	}
	return false
}

// IsFSGroupChangePolicySupported checks if `fsGroupChangePolicy` is supported,
// Supported since 1.20
// https://kubernetes.io/blog/2020/12/14/kubernetes-release-1.20-fsgroupchangepolicy-fsgrouppolicy/#allow-users-to-skip-recursive-permission-changes-on-mount
func IsFSGroupChangePolicySupported() bool {
	if ServerMajorVersion == 1 && ServerMinorVersion >= 20 {
		return true
	}
	return false
}

// NewHPAEmptyObject returns HorizontalPodAutoscaler object
func NewHPAEmptyObject(opts ...func(obj client.Object)) client.Object {
	var hpa client.Object = &v2.HorizontalPodAutoscaler{}
	for _, opt := range opts {
		opt(hpa)
	}
	return hpa
}

// MustConvertObjectVersionsJSON objects with json serialize and deserialize
// it could be used only for converting BETA apis to Stable version
func MustConvertObjectVersionsJSON[A, B any](src *A, objectName string) *B {
	var dst B
	if src == nil {
		return nil
	}
	srcB, err := json.Marshal(src)
	if err != nil {
		log.Error(err, "BUG, cannot serialize object for API", "APIObject", objectName, "object", src)
		return nil
	}
	if err := json.Unmarshal(srcB, &dst); err != nil {
		log.Error(err, "BUG, cannot parse object for API", "APIObject", objectName, "object", string(srcB))
		return nil
	}
	return &dst
}
