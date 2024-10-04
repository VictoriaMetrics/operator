package build

import (
	"fmt"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const probeTimeoutSeconds int32 = 5

type probeCRD interface {
	Probe() *vmv1beta1.EmbeddedProbes
	ProbePath() string
	ProbeScheme() string
	ProbePort() string
	ProbeNeedLiveness() bool
}

// Probe builds probe for container with possible custom values with
func Probe(container corev1.Container, cr probeCRD) corev1.Container {
	// ep *vmv1beta1.EmbeddedProbes, probePath func() string, port string, needAddLiveness bool) corev1.Container {
	var rp, lp, sp *corev1.Probe
	ep := cr.Probe()
	probePath := cr.ProbePath
	port := cr.ProbePort()
	needAddLiveness := cr.ProbeNeedLiveness()
	scheme := cr.ProbeScheme()
	if ep != nil {
		rp = ep.ReadinessProbe
		lp = ep.LivenessProbe
		sp = ep.StartupProbe
	}

	defaultProbeHandler := func() corev1.ProbeHandler {
		return corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port:   intstr.Parse(port),
				Scheme: corev1.URIScheme(scheme),
				Path:   probePath(),
			},
		}
	}
	if rp == nil {
		rp = &corev1.Probe{
			ProbeHandler:     defaultProbeHandler(),
			TimeoutSeconds:   probeTimeoutSeconds,
			PeriodSeconds:    5,
			FailureThreshold: 10,
		}
	}
	if needAddLiveness {
		if lp == nil {
			lp = &corev1.Probe{
				ProbeHandler:     defaultProbeHandler(),
				TimeoutSeconds:   probeTimeoutSeconds,
				FailureThreshold: 10,
				PeriodSeconds:    5,
			}
		}
	}
	// ensure, that custom probe has all needed fields.
	addMissingFields := func(probe *corev1.Probe) {
		if probe != nil {

			if probe.HTTPGet == nil && probe.TCPSocket == nil && probe.Exec == nil {
				probe.HTTPGet = &corev1.HTTPGetAction{
					Port:   intstr.Parse(port),
					Scheme: corev1.URIScheme(scheme),
					Path:   probePath(),
				}
			}
			if probe.HTTPGet != nil {
				if probe.HTTPGet.Path == "" {
					probe.HTTPGet.Path = probePath()
				}
				if probe.HTTPGet.Port.StrVal == "" && probe.HTTPGet.Port.IntVal == 0 {
					probe.HTTPGet.Port = intstr.Parse(port)
				}
			}
			if probe.PeriodSeconds == 0 {
				probe.PeriodSeconds = 5
			}
			if probe.FailureThreshold == 0 {
				probe.FailureThreshold = 10
			}
			if probe.TimeoutSeconds == 0 {
				probe.TimeoutSeconds = probeTimeoutSeconds
			}
			if probe.SuccessThreshold == 0 {
				probe.SuccessThreshold = 1
			}
		}
	}
	addMissingFields(lp)
	addMissingFields(sp)
	addMissingFields(rp)
	container.LivenessProbe = lp
	container.StartupProbe = sp
	container.ReadinessProbe = rp
	return container
}

// Resources creates containter resources with conditional defaults values
func Resources(crdResources corev1.ResourceRequirements, defaultResources config.Resource, useDefault bool) corev1.ResourceRequirements {
	if crdResources.Requests == nil {
		crdResources.Requests = corev1.ResourceList{}
	}
	if crdResources.Limits == nil {
		crdResources.Limits = corev1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := crdResources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := crdResources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}

	if !cpuResourceIsSet && useDefault {
		if defaultResources.Request.Cpu != config.UnLimitedResource {
			crdResources.Requests[corev1.ResourceCPU] = resource.MustParse(defaultResources.Request.Cpu)
		}
		if defaultResources.Limit.Cpu != config.UnLimitedResource {
			crdResources.Limits[corev1.ResourceCPU] = resource.MustParse(defaultResources.Limit.Cpu)
		}
	}
	if !memResourceIsSet && useDefault {
		if defaultResources.Request.Mem != config.UnLimitedResource {
			crdResources.Requests[corev1.ResourceMemory] = resource.MustParse(defaultResources.Request.Mem)
		}
		if defaultResources.Limit.Mem != config.UnLimitedResource {
			crdResources.Limits[corev1.ResourceMemory] = resource.MustParse(defaultResources.Limit.Mem)
		}
	}
	return crdResources
}

// AddExtraArgsOverrideDefaults adds extraArgs for given source args
// it trims in-place args if it was set via extraArgs
// no need to check for extraEnvs, it has priority over args at VictoriaMetrics apps
// dashes is either "-" or "--", depending on the process. altermanager needs two dashes.
func AddExtraArgsOverrideDefaults(args []string, extraArgs map[string]string, dashes string) []string {
	if len(extraArgs) == 0 {
		// fast path
		return args
	}
	cleanArg := func(arg string) string {
		if idx := strings.Index(arg, dashes); idx == 0 {
			arg = arg[len(dashes):]
		}
		idx := strings.IndexByte(arg, '=')
		if idx > 0 {
			arg = arg[:idx]
		}
		return arg
	}
	var cnt int
	for _, arg := range args {
		argKey := cleanArg(arg)
		if _, ok := extraArgs[argKey]; ok {
			continue
		}
		args[cnt] = arg
		cnt++
	}
	// trim in-place
	args = args[:cnt]
	// add extraArgs
	for argKey, argValue := range extraArgs {
		// hack for alertmanager migration
		// TODO remove it at the 28.0 release
		if len(dashes) == 2 && strings.HasPrefix(argKey, "-") {
			argKey = strings.TrimPrefix(argKey, "-")
		}
		// special hack for https://github.com/VictoriaMetrics/VictoriaMetrics/issues/1145
		if argKey == "rule" {
			args = append(args, fmt.Sprintf("%s%s=%q", dashes, argKey, argValue))
		} else {
			args = append(args, fmt.Sprintf("%s%s=%s", dashes, argKey, argValue))
		}
	}
	return args
}

// formatContainerImage returns container image with registry prefix if needed.
func formatContainerImage(globalRepo string, containerImage string) string {
	if globalRepo == "" {
		// no need to add global repo
		return containerImage
	}
	if !strings.HasSuffix(globalRepo, "/") {
		globalRepo += "/"
	}
	// operator has built-in images hosted at quay, check for it.
	if !strings.HasPrefix(containerImage, "quay.io/") {
		return globalRepo + containerImage
	}
	return globalRepo + containerImage[len("quay.io/"):]
}

// AppendInsertPorts conditionally adds ingestPorts to the given ports slice
func AppendInsertPorts(ports []corev1.ContainerPort, ip *vmv1beta1.InsertPorts) []corev1.ContainerPort {
	if ip == nil {
		return ports
	}
	if ip.GraphitePort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "graphite-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.GraphitePort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "graphite-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.GraphitePort).IntVal,
			},
		)
	}
	if ip.InfluxPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "influx-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.InfluxPort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "influx-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.InfluxPort).IntVal,
			},
		)
	}
	if ip.OpenTSDBPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "opentsdb-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.OpenTSDBPort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "opentsdb-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.OpenTSDBPort).IntVal,
			},
		)
	}
	if ip.OpenTSDBHTTPPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "opentsdb-http",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.OpenTSDBHTTPPort).IntVal,
			},
		)
	}
	return ports
}

// AppendArgsForInsertPorts conditionally appends insert ports as flags to the given args
func AppendArgsForInsertPorts(args []string, ip *vmv1beta1.InsertPorts) []string {
	if ip == nil {
		return args
	}
	if ip.GraphitePort != "" {
		args = append(args, fmt.Sprintf("--graphiteListenAddr=:%s", ip.GraphitePort))
	}
	if ip.InfluxPort != "" {
		args = append(args, fmt.Sprintf("--influxListenAddr=:%s", ip.InfluxPort))
	}
	if ip.OpenTSDBPort != "" {
		args = append(args, fmt.Sprintf("--opentsdbListenAddr=:%s", ip.OpenTSDBPort))
	}
	if ip.OpenTSDBHTTPPort != "" {
		args = append(args, fmt.Sprintf("--opentsdbHTTPListenAddr=:%s", ip.OpenTSDBHTTPPort))
	}
	return args
}

var (
	configReloaderDefaultPort    = 8435
	configReloaderContainerProbe = corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path:   "/health",
			Scheme: "HTTP",
			Port:   intstr.FromInt(configReloaderDefaultPort),
		},
	}
)

// AddsPortProbesToConfigReloaderContainer conditionally adds readiness and liveness probes to the custom config-reloader image
// exposes reloader-http port for container
func AddsPortProbesToConfigReloaderContainer(useCustomConfigReloader bool, crContainer *corev1.Container) {
	if !useCustomConfigReloader {
		return
	}
	crContainer.Ports = append(crContainer.Ports, corev1.ContainerPort{
		ContainerPort: int32(configReloaderDefaultPort),
		Name:          "reloader-http",
		Protocol:      "TCP",
	})
	crContainer.LivenessProbe = &corev1.Probe{
		TimeoutSeconds:   1,
		SuccessThreshold: 1,
		FailureThreshold: 3,
		PeriodSeconds:    10,
		ProbeHandler:     configReloaderContainerProbe,
	}
	crContainer.ReadinessProbe = &corev1.Probe{
		InitialDelaySeconds: 5,
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		PeriodSeconds:       10,
		ProbeHandler:        configReloaderContainerProbe,
	}
}
