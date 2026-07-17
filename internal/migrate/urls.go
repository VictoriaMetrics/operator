package migrate

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func envFlagEnabled(args []string) bool {
	enabled := false
	for _, arg := range args {
		if arg == "-envflag.enable" {
			enabled = true
			continue
		}
		if v, ok := strings.CutPrefix(arg, "-envflag.enable="); ok {
			if parsed, err := strconv.ParseBool(v); err == nil {
				enabled = parsed
			}
		}
	}
	return enabled
}

func envFlagPrefixCandidates(args []string) []string {
	prefix, found := "", false
	for _, arg := range args {
		if v, ok := strings.CutPrefix(arg, "-envflag.prefix="); ok {
			prefix, found = v, true
		}
	}
	if found {
		return []string{prefix}
	}
	return []string{""}
}

func envFlagName(prefix, flagName string) string {
	return prefix + strings.ReplaceAll(flagName, ".", "_")
}

// lastEnvVar returns the last entry named name in env, matching the container runtime's
// last-value-wins semantics for duplicate env var names.
func lastEnvVar(env []corev1.EnvVar, name string) *corev1.EnvVar {
	var last *corev1.EnvVar
	for i := range env {
		if env[i].Name == name {
			last = &env[i]
		}
	}
	return last
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
		argValue, argSet := "", false
		for _, arg := range c.Args {
			if v, ok := strings.CutPrefix(arg, flag); ok {
				argValue, argSet = v, true
			}
		}
		if argSet {
			if argValue != "" {
				return true
			}
			continue
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return true
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			authKeyEnvName := envFlagName(prefix, "forceMergeAuthKey")
			if e := lastEnvVar(c.Env, authKeyEnvName); e != nil && (e.ValueFrom != nil || e.Value != "") {
				return true
			}
		}
	}
	return false
}

func rejectTLSEnabled(namespace, name string, containers []corev1.Container) error {
	for _, c := range containers {
		tlsArg, tlsArgSet := "", false
		for _, arg := range c.Args {
			if arg == "-tls" || strings.HasPrefix(arg, "-tls=") {
				tlsArg, tlsArgSet = arg, true
			}
		}
		if tlsArgSet {
			if tlsArg == "-tls" {
				return fmt.Errorf("component %s/%s has TLS enabled (%q) — the buffer agent can only forward over plain HTTP, so migration isn't supported for it", namespace, name, tlsArg)
			}
			v := strings.TrimPrefix(tlsArg, "-tls=")
			if enabled, err := arrayBoolAnyEnabled(v); err != nil || enabled {
				return fmt.Errorf("component %s/%s has TLS enabled (%q) — the buffer agent can only forward over plain HTTP, so migration isn't supported for it", namespace, name, tlsArg)
			}
			continue
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return fmt.Errorf("component %s/%s reads environment-sourced flags (-envflag.enable) and also imports env vars via envFrom, which this tool can't inspect to rule out TLS being enabled that way — refusing to guess; remove the envFrom import or set -tls directly instead", namespace, name)
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			tlsEnvName := envFlagName(prefix, "tls")
			e := lastEnvVar(c.Env, tlsEnvName)
			if e == nil {
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
	return nil
}

func httpPathPrefix(containers []corev1.Container) (string, error) {
	const flag = "-http.pathPrefix="
	for _, c := range containers {
		argValue, argSet := "", false
		for _, arg := range c.Args {
			if p, ok := strings.CutPrefix(arg, flag); ok {
				argValue, argSet = p, true
			}
		}
		if argSet {
			return normalizePathPrefix(argValue), nil
		}
		if !envFlagEnabled(c.Args) {
			continue
		}
		if len(c.EnvFrom) > 0 {
			return "", fmt.Errorf("container %q reads environment-sourced flags (-envflag.enable) and also imports env vars via envFrom, which this tool can't inspect to resolve http.pathPrefix — refusing to guess", c.Name)
		}
		for _, prefix := range envFlagPrefixCandidates(c.Args) {
			pathPrefixEnvName := envFlagName(prefix, "http.pathPrefix")
			e := lastEnvVar(c.Env, pathPrefixEnvName)
			if e == nil {
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
