package k8stools

import (
	"fmt"
	"k8s.io/apimachinery/pkg/version"
	"strconv"
	"strings"
)

var (
	ServerMajorVersion uint64
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
