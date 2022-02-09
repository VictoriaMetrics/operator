package k8stools

import (
	"fmt"
	"k8s.io/apimachinery/pkg/version"
	"strconv"
)

var (
	ServerMajorVersion uint64
	ServerMinorVersion uint64
)

// TrySetKubernetesServerVersion parses kubernetes
func TrySetKubernetesServerVersion(vi *version.Info) error {
	v, err := strconv.ParseUint(vi.Minor, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse kubernetes server minor version: %q as uint: %w", vi.Minor, err)
	}
	ServerMinorVersion = v
	v, err = strconv.ParseUint(vi.Major, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse kubernetes server major version: %q as uint: %w", vi.Major, err)
	}
	ServerMajorVersion = v
	return nil
}

// IsPSPSupported check if PodSecurityPolicy is supported by kubernetes API server
// https://kubernetes.io/docs/reference/using-api/deprecation-guide/#psp-v125
func IsPSPSupported() bool {
	if ServerMajorVersion == 1 && ServerMinorVersion < 23 {
		return false
	}
	return true
}

// IsPDBV1APISupported check if new v1 API is supported by kubernetes API server
// deprecated since 1.21
// https://kubernetes.io/docs/reference/using-api/deprecation-guide/#poddisruptionbudget-v125
func IsPDBV1APISupported() bool {
	if ServerMajorVersion == 1 && ServerMinorVersion <= 21 {
		return false
	}
	return true
}
