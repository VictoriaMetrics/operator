package migrate

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func envFlagEnabled(args []string) bool {
	for _, arg := range args {
		if arg == "-envflag.enable" {
			return true
		}
		if v, ok := strings.CutPrefix(arg, "-envflag.enable="); ok {
			if enabled, err := strconv.ParseBool(v); err == nil && enabled {
				return true
			}
		}
	}
	return false
}

func envFlagPrefixCandidates(args []string) []string {
	for _, arg := range args {
		if v, ok := strings.CutPrefix(arg, "-envflag.prefix="); ok {
			return []string{v}
		}
	}
	return []string{""}
}

func envFlagName(prefix, flagName string) string {
	return prefix + strings.ReplaceAll(flagName, ".", "_")
}

func arrayBoolAnyEnabled(value string) (bool, error) {
	for _, v := range strings.Split(value, ",") {
		if v == "" {
			continue
		}
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return false, err
		}
		if enabled {
			return true, nil
		}
	}
	return false, nil
}

func hasForceMergeAuthKey(containers []corev1.Container) bool {
	const flag = "-forceMergeAuthKey="
	for _, c := range containers {
		for _, arg := range c.Args {
			if v, ok := strings.CutPrefix(arg, flag); ok && v != "" {
				return true
			}
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return true
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			authKeyEnvName := envFlagName(prefix, "forceMergeAuthKey")
			for _, e := range c.Env {
				if e.Name == authKeyEnvName && (e.ValueFrom != nil || e.Value != "") {
					return true
				}
			}
		}
	}
	return false
}

func rejectTLSEnabled(namespace, name string, containers []corev1.Container) error {
	for _, c := range containers {
		for _, arg := range c.Args {
			if arg == "-tls" {
				return fmt.Errorf("component %s/%s has TLS enabled (%q) — the buffer agent can only forward over plain HTTP, so migration isn't supported for it", namespace, name, arg)
			}
			if v, ok := strings.CutPrefix(arg, "-tls="); ok {
				if enabled, err := arrayBoolAnyEnabled(v); err != nil || enabled {
					return fmt.Errorf("component %s/%s has TLS enabled (%q) — the buffer agent can only forward over plain HTTP, so migration isn't supported for it", namespace, name, arg)
				}
			}
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return fmt.Errorf("component %s/%s reads environment-sourced flags (-envflag.enable) and also imports env vars via envFrom, which this tool can't inspect to rule out TLS being enabled that way — refusing to guess; remove the envFrom import or set -tls directly instead", namespace, name)
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			tlsEnvName := envFlagName(prefix, "tls")
			for _, e := range c.Env {
				if e.Name != tlsEnvName {
					continue
				}
				if e.ValueFrom != nil {
					return fmt.Errorf("component %s/%s sets %s via valueFrom, which this tool can't resolve to rule out TLS being enabled — refusing to guess; use a literal env value instead", namespace, name, tlsEnvName)
				}
				if enabled, err := arrayBoolAnyEnabled(e.Value); err != nil || enabled {
					return fmt.Errorf("component %s/%s has TLS enabled (%s=%q env var) — the buffer agent can only forward over plain HTTP, so migration isn't supported for it", namespace, name, tlsEnvName, e.Value)
				}
			}
		}
	}
	return nil
}

func httpPathPrefix(containers []corev1.Container) (string, error) {
	const flag = "-http.pathPrefix="
	for _, c := range containers {
		for _, arg := range c.Args {
			if p, ok := strings.CutPrefix(arg, flag); ok {
				return normalizePathPrefix(p), nil
			}
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return "", fmt.Errorf("container %q reads environment-sourced flags (-envflag.enable) and also imports env vars via envFrom, which this tool can't inspect to resolve http.pathPrefix — refusing to guess", c.Name)
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			pathPrefixEnvName := envFlagName(prefix, "http.pathPrefix")
			for _, e := range c.Env {
				if e.Name != pathPrefixEnvName {
					continue
				}
				if e.ValueFrom != nil {
					return "", fmt.Errorf("container %q sets %s via valueFrom, which this tool can't resolve http.pathPrefix from — refusing to guess", c.Name, pathPrefixEnvName)
				}
				if e.Value != "" {
					return normalizePathPrefix(e.Value), nil
				}
			}
		}
	}
	return "", nil
}

func normalizePathPrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

func serviceHTTPPort(svc *corev1.Service, portName string) (int32, bool) {
	for _, p := range svc.Spec.Ports {
		if p.Name == portName {
			return p.Port, true
		}
	}
	return 0, false
}

func serviceBaseURL(svc *corev1.Service, scheme, portName string) (string, error) {
	port, ok := serviceHTTPPort(svc, portName)
	if !ok {
		return "", fmt.Errorf("service %s/%s has no %q port", svc.Namespace, svc.Name, portName)
	}
	return fmt.Sprintf("%s://%s.%s.svc:%d", scheme, svc.Name, svc.Namespace, port), nil
}

func forceMergeURL(svc *corev1.Service, pathPrefix string) (string, error) {
	base, err := serviceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + pathPrefix + "/internal/force_merge", nil
}
