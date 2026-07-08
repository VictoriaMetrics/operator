package build

import (
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const probeTimeoutSeconds int32 = 5
const DataVolumeName = "data"

type probeCRD interface {
	ProbePath() string
	ProbeScheme() string
	ProbePort() string
	ProbeNeedLiveness() bool
	UseProxyProtocol() bool
}

// Probe adds probe for container
func Probe(container *corev1.Container, cr probeCRD, params *vmv1beta1.CommonAppsParams) {
	port := intstr.Parse(cr.ProbePort())
	scheme := cr.ProbeScheme()
	path := cr.ProbePath()
	getProbe := func(probe *corev1.Probe, createIfNil bool, forceTCP bool) *corev1.Probe {
		if probe == nil {
			if createIfNil {
				probe = new(corev1.Probe)
			} else {
				return probe
			}
		}
		if probe.HTTPGet == nil && probe.TCPSocket == nil && probe.Exec == nil {
			if forceTCP || cr.UseProxyProtocol() {
				probe.TCPSocket = new(corev1.TCPSocketAction)
			} else {
				probe.HTTPGet = new(corev1.HTTPGetAction)
			}
		}
		if probe.TCPSocket != nil {
			if probe.TCPSocket.Port.StrVal == "" && probe.TCPSocket.Port.IntVal == 0 {
				probe.TCPSocket.Port = port
			}
		} else if probe.HTTPGet != nil {
			if len(probe.HTTPGet.Path) == 0 {
				probe.HTTPGet.Path = path
			}
			if probe.HTTPGet.Port.StrVal == "" && probe.HTTPGet.Port.IntVal == 0 {
				probe.HTTPGet.Port = port
			}
			if len(probe.HTTPGet.Scheme) == 0 {
				probe.HTTPGet.Scheme = corev1.URIScheme(scheme)
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
		return probe
	}
	var liveness, readiness, startup *corev1.Probe
	if params != nil {
		liveness, readiness, startup = params.LivenessProbe, params.ReadinessProbe, params.StartupProbe
	}
	hasCustomProbe := liveness != nil || readiness != nil || startup != nil
	if !hasCustomProbe && scheme == string(corev1.URISchemeHTTPS) {
		container.StartupProbe = getProbe(nil, true, true)
		return
	}
	if cr.ProbeNeedLiveness() {
		container.LivenessProbe = getProbe(liveness, true, false)
	}
	container.StartupProbe = getProbe(startup, false, true)
	container.ReadinessProbe = getProbe(readiness, true, false)
}

// Resources creates container resources with conditional defaults values
func Resources(crdResources corev1.ResourceRequirements, defaultResources config.Resource, useDefault bool) corev1.ResourceRequirements {
	if crdResources.Requests == nil {
		crdResources.Requests = corev1.ResourceList{}
	}
	if crdResources.Limits == nil {
		crdResources.Limits = corev1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool
	var ephemeralStorageResourceIsSet bool

	if _, ok := crdResources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := crdResources.Limits[corev1.ResourceEphemeralStorage]; ok {
		ephemeralStorageResourceIsSet = true
	}
	if _, ok := crdResources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := crdResources.Requests[corev1.ResourceEphemeralStorage]; ok {
		ephemeralStorageResourceIsSet = true
	}

	if !cpuResourceIsSet && useDefault {
		if defaultResources.Request.Cpu != config.UnlimitedQuantity {
			crdResources.Requests[corev1.ResourceCPU] = resource.MustParse(defaultResources.Request.Cpu)
		}
		if defaultResources.Limit.Cpu != config.UnlimitedQuantity {
			crdResources.Limits[corev1.ResourceCPU] = resource.MustParse(defaultResources.Limit.Cpu)
		}
	}
	if !memResourceIsSet && useDefault {
		if defaultResources.Request.Mem != config.UnlimitedQuantity {
			crdResources.Requests[corev1.ResourceMemory] = resource.MustParse(defaultResources.Request.Mem)
		}
		if defaultResources.Limit.Mem != config.UnlimitedQuantity {
			crdResources.Limits[corev1.ResourceMemory] = resource.MustParse(defaultResources.Limit.Mem)
		}
	}
	if !ephemeralStorageResourceIsSet && useDefault {
		if defaultResources.Request.EphemeralStorage != config.UnlimitedQuantity {
			crdResources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse(defaultResources.Request.EphemeralStorage)
		}
		if defaultResources.Limit.EphemeralStorage != config.UnlimitedQuantity {
			crdResources.Limits[corev1.ResourceEphemeralStorage] = resource.MustParse(defaultResources.Limit.EphemeralStorage)
		}
	}
	return crdResources
}

// AddExtraArgsOverrideDefaults adds extraArgs for given source args
// it trims in-place args if it was set via extraArgs
// no need to check for extraEnvs, it has priority over args at VictoriaMetrics apps
// dashes is either "-" or "--", depending on the process. alertmanager needs two dashes.
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

// Lifecycle applies default lifecycle hooks to the container from params.
// It adds a preStop sleep hook to prevent traffic loss during pod termination,
// unless the user has already set a preStop hook or preStopSleepSeconds is zero.
// Requires Kubernetes >= 1.29 (PodLifecycleSleepAction feature gate).
func Lifecycle(container *corev1.Container, params *vmv1beta1.CommonAppsParams) {
	if params == nil || params.PreStopSleepSeconds == nil || *params.PreStopSleepSeconds <= 0 {
		return
	}
	if !k8stools.IsPodLifecycleSleepActionSupported() {
		return
	}
	// Skip if the sleep would meet or exceed the termination grace period — the pod
	// would receive SIGKILL during the hook, defeating its purpose.
	if params.TerminationGracePeriodSeconds != nil && int64(*params.PreStopSleepSeconds) >= *params.TerminationGracePeriodSeconds {
		return
	}
	if container.Lifecycle == nil {
		container.Lifecycle = &corev1.Lifecycle{}
	}
	if container.Lifecycle.PreStop == nil {
		container.Lifecycle.PreStop = &corev1.LifecycleHandler{
			Sleep: &corev1.SleepAction{
				Seconds: int64(*params.PreStopSleepSeconds),
			},
		}
	}
}

// formatContainerImage returns container image with registry prefix if needed.
func formatContainerImage(registry string, containerImage string) string {
	if registry == "" {
		// no need to add global repo
		return containerImage
	}
	if !strings.HasSuffix(registry, "/") {
		registry += "/"
	}
	// getting location of repo/image separator
	if idx := strings.LastIndex(containerImage, "/"); idx != -1 {
		// getting location of registry/repo separator
		if idx = strings.LastIndex(containerImage[:idx], "/"); idx != -1 {
			return containerImage
		}
	}
	return registry + containerImage
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

const (
	// ConfigReloaderDefaultPort is the config-reloader container's fixed HTTP listen port,
	// exposing its own /health and /metrics endpoints.
	ConfigReloaderDefaultPort = 8435
	// ConfigReloaderPortName is the container/service port name for ConfigReloaderDefaultPort.
	ConfigReloaderPortName = "reloader-http"
)

var configReloaderContainerProbe = corev1.ProbeHandler{
	HTTPGet: &corev1.HTTPGetAction{
		Path:   "/health",
		Scheme: "HTTP",
		Port:   intstr.FromInt(ConfigReloaderDefaultPort),
	},
}

func configReloaderJobRelabeling() vmv1beta1.EndpointRelabelings {
	return vmv1beta1.EndpointRelabelings{
		RelabelConfigs: []*vmv1beta1.RelabelConfig{
			{
				SourceLabels: []string{"job"},
				TargetLabel:  "job",
				Regex:        vmv1beta1.StringOrArray{"(.+)"},
				Replacement:  ptr.To("${1}-" + ConfigReloaderPortName),
			},
		},
	}
}

// ConfigReloaderVMServiceScrapeEndpoint returns a VMServiceScrape endpoint that scrapes the
// config-reloader sidecar directly by its container port (via TargetPort, resolved from the
// pod's actual EndpointSlice ports), without requiring a matching named ServicePort.
func ConfigReloaderVMServiceScrapeEndpoint() vmv1beta1.Endpoint {
	return vmv1beta1.Endpoint{
		TargetPort:          ptr.To(intstr.FromInt32(ConfigReloaderDefaultPort)),
		EndpointRelabelings: configReloaderJobRelabeling(),
		EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
			Path: "/metrics",
		},
	}
}

// ConfigReloaderPodScrapeEndpoint returns a VMPodScrape endpoint that scrapes the
// config-reloader sidecar directly by its container port number.
func ConfigReloaderPodScrapeEndpoint() vmv1beta1.PodMetricsEndpoint {
	return vmv1beta1.PodMetricsEndpoint{
		PortNumber:          ptr.To(int32(ConfigReloaderDefaultPort)),
		EndpointRelabelings: configReloaderJobRelabeling(),
		EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
			Path: "/metrics",
		},
	}
}

type reloadable interface {
	GetReloaderParams() *vmv1beta1.CommonConfigReloaderParams
	GetReloadURL(string) string
	GetNamespace() string
	UseProxyProtocol() bool
	AutomountServiceAccountToken() bool
}

func ConfigReloaderContainer(isInit bool, cr reloadable, mounts []corev1.VolumeMount, ss *corev1.SecretKeySelector) corev1.Container {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("--reload-url=%s", cr.GetReloadURL(config.GetLocalhost())),
		"--webhook-method=POST",
	}
	if cfg.EnableTCP6 {
		args = append(args, "--enableTCP6")
	}
	if cr.UseProxyProtocol() {
		args = append(args, "--reload-use-proxy-protocol")
	}
	outVolumeName := "config-out"
	if ss != nil {
		var configDir string
		for _, m := range mounts {
			if m.Name == outVolumeName {
				configDir = m.MountPath
			}
		}
		args = append(args,
			fmt.Sprintf("--config-envsubst-file=%s", filepath.Join(configDir, ss.Key)),
			fmt.Sprintf("--config-secret-name=%s/%s", cr.GetNamespace(), ss.Name),
			fmt.Sprintf("--config-secret-key=%s.gz", ss.Key))
	}
	if isInit {
		args = append(args, "--only-init-config")
	} else {
		for _, m := range mounts {
			if m.Name != outVolumeName {
				args = append(args, fmt.Sprintf("--watched-dir=%s", m.MountPath))
			}
		}
	}

	p := cr.GetReloaderParams()
	if len(p.ConfigReloaderExtraArgs) > 0 {
		newArgs := args[:0]
		for _, arg := range args {
			argName := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if _, ok := p.ConfigReloaderExtraArgs[argName]; !ok {
				newArgs = append(newArgs, arg)
			}
		}
		for k, v := range p.ConfigReloaderExtraArgs {
			newArgs = append(newArgs, fmt.Sprintf(`--%s=%s`, k, v))
		}
		args = newArgs
	}
	sort.Strings(args)
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].Name < mounts[j].Name
	})
	c := corev1.Container{
		Name:         "config-init",
		Image:        p.ConfigReloaderImage,
		Args:         args,
		Resources:    p.ConfigReloaderResources,
		VolumeMounts: mounts,
	}
	if ss != nil {
		AddServiceAccountTokenVolumeMount(&c, cr.AutomountServiceAccountToken())
	}
	if !isInit {
		c.Name = "config-reloader"
		c.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
		addPortProbesToConfigReloaderContainer(&c)
		addConfigReloadAuthKeyToReloader(&c, p)
	}
	return c
}

// addPortProbesToConfigReloaderContainer conditionally adds readiness and liveness probes to the custom config-reloader image
// exposes reloader-http port for container
func addPortProbesToConfigReloaderContainer(crContainer *corev1.Container) {
	crContainer.Ports = append(crContainer.Ports, corev1.ContainerPort{
		ContainerPort: int32(ConfigReloaderDefaultPort),
		Name:          ConfigReloaderPortName,
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

const (
	authKeyAppFlagName      = "reloadAuthKey"
	authKeyReloaderFlagName = "reload-url-auth-key"
	authKeyMountPath        = "/etc/vm/config-reload-auth"
	authKeyMountName        = "vm-config-reload-auth-secret"
)

// AddConfigReloadAuthKeyVolume conditionally adds volume with config reload auth secret
func AddConfigReloadAuthKeyVolume(dst []corev1.Volume, spec *vmv1beta1.CommonConfigReloaderParams) []corev1.Volume {
	if spec.ConfigReloadAuthKeySecret == nil {
		return dst
	}
	dst = append(dst, corev1.Volume{
		Name: authKeyMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: spec.ConfigReloadAuthKeySecret.Name,
			},
		},
	})
	return dst
}

// AddConfigReloadAuthKeyToApp adds authKey env var to the given application container
func AddConfigReloadAuthKeyToApp(container *corev1.Container, extraArgs map[string]string, spec *vmv1beta1.CommonConfigReloaderParams) {
	if spec.ConfigReloadAuthKeySecret == nil {
		return
	}
	container.Args = append(container.Args, fmt.Sprintf("-%s=file://%s/%s", authKeyAppFlagName, authKeyMountPath, spec.ConfigReloadAuthKeySecret.Key))
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      authKeyMountName,
		MountPath: authKeyMountPath,
	})
}

// AddWatchTargetDir mounts srcMount into c and appends the matching --watched-dir / --target-dir
// flag pair atomically. It is a no-op if srcMount.MountPath is already present in c.VolumeMounts,
// preventing duplicate mounts when the same directory appears in both crMounts and the rule dirs.
func AddWatchTargetDir(c *corev1.Container, srcMount corev1.VolumeMount, targetDir string) {
	for _, m := range c.VolumeMounts {
		if m.MountPath == srcMount.MountPath {
			return
		}
	}
	c.VolumeMounts = append(c.VolumeMounts, srcMount)
	c.Args = append(c.Args,
		fmt.Sprintf("--watched-dir=%s", srcMount.MountPath),
		fmt.Sprintf("--target-dir=%s", targetDir),
	)
}

// addConfigReloadAuthKeyToReloader adds authKey env var to the given config-reloader container
func addConfigReloadAuthKeyToReloader(container *corev1.Container, spec *vmv1beta1.CommonConfigReloaderParams) {
	if spec.ConfigReloadAuthKeySecret == nil {
		return
	}
	container.Args = append(container.Args, fmt.Sprintf("-%s=file://%s/%s", authKeyReloaderFlagName, authKeyMountPath, spec.ConfigReloadAuthKeySecret.Key))
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      authKeyMountName,
		MountPath: authKeyMountPath,
	})
}

// AddSyslogPortsTo adds syslog ports into provided dst
func AddSyslogPortsTo(dst []corev1.ContainerPort, syslogSpec *vmv1.SyslogServerSpec) []corev1.ContainerPort {
	if syslogSpec == nil {
		return dst
	}
	for idx, tcp := range syslogSpec.TCPListeners {
		dst = append(dst, corev1.ContainerPort{
			Name:          fmt.Sprintf("syslog-tcp-%d", idx),
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: tcp.ListenPort,
		})
	}

	for idx, udp := range syslogSpec.UDPListeners {
		dst = append(dst, corev1.ContainerPort{
			Name:          fmt.Sprintf("syslog-udp-%d", idx),
			Protocol:      corev1.ProtocolUDP,
			ContainerPort: udp.ListenPort,
		},
		)
	}
	return dst
}

// AddSyslogArgsTo adds syslog flag args into provided dst
func AddSyslogArgsTo(dst []string, syslogSpec *vmv1.SyslogServerSpec, tlsMountPath string) []string {
	if syslogSpec == nil {
		return dst
	}

	tcpListenAddr := NewEmptyFlag("-syslog.listenAddr.tcp")
	tcpStreamFields := NewFlag("-syslog.streamFields.tcp", "''")
	tcpIgnoreFields := NewFlag("-syslog.ignoreFields.tcp", "''")
	tcpDecolorizedFields := NewFlag("-syslog.decolorizeFields.tcp", "''")
	tcpTenantID := NewEmptyFlag("-syslog.tenantID.tcp")
	tcpCompress := NewEmptyFlag("-syslog.compressMethod.tcp")
	tlsEnabled := NewEmptyFlag("-syslog.tls")
	tlsCertFile := NewEmptyFlag("-syslog.tlsCertFile")
	tlsKeyFile := NewEmptyFlag("-syslog.tlsKeyFile")
	tlsCipherSuites := NewEmptyFlag("-syslog.tlsCipherSuites")

	var value, tlsMinVersion string

	for idx, sTCP := range syslogSpec.TCPListeners {
		tcpListenAddr.Add(fmt.Sprintf(":%d", sTCP.ListenPort), idx)
		tcpStreamFields.Add(fmt.Sprintf("'%s'", sTCP.StreamFields), idx)
		tcpDecolorizedFields.Add(fmt.Sprintf("'%s'", sTCP.DecolorizeFields), idx)
		tcpIgnoreFields.Add(fmt.Sprintf("'%s'", sTCP.IgnoreFields), idx)
		tcpTenantID.Add(sTCP.TenantID, idx)
		tcpCompress.Add(sTCP.CompressMethod, idx)

		if sTCP.TLSConfig != nil {
			tlsEnabled.Add("true", idx)
			tlsC := sTCP.TLSConfig
			value = ""
			switch {
			case tlsC.CertFile != "":
				value = tlsC.CertFile
			case tlsC.CertSecret != nil:
				value = fmt.Sprintf("%s/%s/%s", tlsMountPath, tlsC.CertSecret.Name, tlsC.CertSecret.Key)
			}
			tlsCertFile.Add(value, idx)
			value = ""
			switch {
			case tlsC.KeyFile != "":
				value = tlsC.KeyFile
			case tlsC.KeySecret != nil:
				value = fmt.Sprintf("%s/%s/%s", tlsMountPath, tlsC.KeySecret.Name, tlsC.KeySecret.Key)
			}
			tlsKeyFile.Add(value, idx)
			if len(tlsC.CipherSuites) > 0 {
				cs := strings.ReplaceAll(strings.Join(tlsC.CipherSuites, ","), `\`, `\\`)
				cs = strings.ReplaceAll(cs, "'", `\'`)
				tlsCipherSuites.Add(fmt.Sprintf("'%s'", cs), idx)
			} else {
				tlsCipherSuites.Add("", idx)
			}
			if tlsMinVersion == "" && tlsC.MinVersion != "" {
				tlsMinVersion = tlsC.MinVersion
			}
		}
	}
	dst = AppendFlagsToArgs(dst, len(syslogSpec.TCPListeners), tcpListenAddr, tcpStreamFields, tcpIgnoreFields, tcpDecolorizedFields, tcpTenantID, tcpCompress, tlsEnabled, tlsCertFile, tlsKeyFile, tlsCipherSuites)
	if tlsMinVersion != "" {
		dst = append(dst, fmt.Sprintf("-syslog.tlsMinVersion=%s", tlsMinVersion))
	}

	udpListenAddr := NewEmptyFlag("-syslog.listenAddr.udp")
	udpStreamFileds := NewFlag("-syslog.streamFields.udp", "''")
	udpIgnoreFields := NewFlag("-syslog.ignoreFields.udp", "''")
	udpDecolorizedFields := NewFlag("-syslog.decolorizeFields.udp", "''")
	udpTenantID := NewEmptyFlag("-syslog.tenantID.udp")
	udpCompress := NewEmptyFlag("-syslog.compressMethod.udp")

	for idx, sUDP := range syslogSpec.UDPListeners {
		udpListenAddr.Add(fmt.Sprintf(":%d", sUDP.ListenPort), idx)
		udpStreamFileds.Add(fmt.Sprintf("'%s'", sUDP.StreamFields), idx)
		udpIgnoreFields.Add(fmt.Sprintf("'%s'", sUDP.IgnoreFields), idx)
		udpDecolorizedFields.Add(fmt.Sprintf("'%s'", sUDP.DecolorizeFields), idx)
		udpTenantID.Add(sUDP.TenantID, idx)
		udpCompress.Add(sUDP.CompressMethod, idx)
	}

	dst = AppendFlagsToArgs(dst, len(syslogSpec.UDPListeners), udpListenAddr, udpStreamFileds, udpIgnoreFields, udpDecolorizedFields, udpTenantID, udpCompress)

	return dst
}

// addTLSSecretVolume adds a secret volume+mount for sr, deduped by secret name across callers.
func addTLSSecretVolume(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, sr *corev1.SecretKeySelector, tlsMountPath string) ([]corev1.Volume, []corev1.VolumeMount) {
	name := k8stools.SanitizeVolumeName(fmt.Sprintf("tls-%s", sr.Name))
	for _, dst := range dstVolumes {
		if dst.Name == name && dst.Secret != nil && dst.Secret.SecretName == sr.Name {
			return dstVolumes, dstMounts
		}
	}
	dstVolumes = append(dstVolumes, corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: sr.Name},
		},
	})
	dstMounts = append(dstMounts, corev1.VolumeMount{
		Name:      name,
		MountPath: fmt.Sprintf("%s/%s", tlsMountPath, sr.Name),
	})
	return dstVolumes, dstMounts
}

// AddSyslogTLSConfigToVolumes adds syslog tlsConfig volumes and mounts to the provided dsts
func AddSyslogTLSConfigToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, syslogSpec *vmv1.SyslogServerSpec, tlsMountPath string) ([]corev1.Volume, []corev1.VolumeMount) {
	if syslogSpec == nil || len(syslogSpec.TCPListeners) == 0 {
		return dstVolumes, dstMounts
	}

	addSecret := func(sr *corev1.SecretKeySelector) {
		dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, sr, tlsMountPath)
	}
	for _, tc := range syslogSpec.TCPListeners {
		if tc.TLSConfig == nil {
			continue
		}
		tlsC := tc.TLSConfig
		switch {
		case tlsC.CertFile != "":
		case tlsC.CertSecret != nil:
			addSecret(tlsC.CertSecret)
		}

		switch {
		case tlsC.KeyFile != "":
		case tlsC.KeySecret != nil:
			addSecret(tlsC.KeySecret)
		}

	}
	return dstVolumes, dstMounts
}

// AddOTLPGRPCPortTo adds the OTLP gRPC container port if grpcSpec is configured
func AddOTLPGRPCPortTo(dst []corev1.ContainerPort, grpcSpec *vmv1.OTLPGRPCSpec) []corev1.ContainerPort {
	if grpcSpec == nil {
		return dst
	}
	return append(dst, corev1.ContainerPort{
		Name:          vmv1.OTLPGRPCPortName,
		Protocol:      corev1.ProtocolTCP,
		ContainerPort: grpcSpec.ListenPort,
	})
}

// AddOTLPGRPCArgsTo adds otlpGRPC flag args into provided dst
func AddOTLPGRPCArgsTo(dst []string, grpcSpec *vmv1.OTLPGRPCSpec, tlsMountPath string) []string {
	if grpcSpec == nil {
		return dst
	}
	dst = append(dst, fmt.Sprintf("-otlpGRPCListenAddr=:%d", grpcSpec.ListenPort))

	tlsC := grpcSpec.TLSConfig
	if tlsC == nil {
		return dst
	}
	dst = append(dst, "-otlpGRPC.tls=true")
	switch {
	case tlsC.CertFile != "":
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsCertFile=%s", tlsC.CertFile))
	case tlsC.CertSecret != nil:
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsCertFile=%s/%s/%s", tlsMountPath, tlsC.CertSecret.Name, tlsC.CertSecret.Key))
	}
	switch {
	case tlsC.KeyFile != "":
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsKeyFile=%s", tlsC.KeyFile))
	case tlsC.KeySecret != nil:
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsKeyFile=%s/%s/%s", tlsMountPath, tlsC.KeySecret.Name, tlsC.KeySecret.Key))
	}
	if tlsC.MinVersion != "" {
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsMinVersion=%s", tlsC.MinVersion))
	}
	if len(tlsC.CipherSuites) > 0 {
		dst = append(dst, fmt.Sprintf("-otlpGRPC.tlsCipherSuites=%s", strings.Join(tlsC.CipherSuites, ",")))
	}
	return dst
}

// AddHTTPListenerArgsTo adds httpListenAddr and related positional flags from HTTPListeners into dst.
// It should only be called when ExtraArgs["httpListenAddr"] is not set.
func AddHTTPListenerArgsTo(dst []string, listeners []vmv1beta1.HTTPListener, tlsMountPath string) []string {
	if len(listeners) == 0 {
		return dst
	}
	listenAddr := NewEmptyFlag("-httpListenAddr")
	tls := NewEmptyFlag("-tls")
	tlsCertFile := NewEmptyFlag("-tlsCertFile")
	tlsKeyFile := NewEmptyFlag("-tlsKeyFile")
	tlsMinVersion := NewEmptyFlag("-tlsMinVersion")
	autocertHosts := NewEmptyFlag("-tlsAutocertHosts")
	autocertEmail := NewEmptyFlag("-tlsAutocertEmail")
	autocertDir := NewEmptyFlag("-tlsAutocertCacheDir")
	mtls := NewEmptyFlag("-mtls")
	mtlsCAFile := NewEmptyFlag("-mtlsCAFile")
	proxyProto := NewEmptyFlag("-httpListenAddr.useProxyProtocol")

	for idx, hl := range listeners {
		listenAddr.Add(hl.Addr, idx)
		if hl.TLS != nil {
			tls.Add(strconv.FormatBool(*hl.TLS), idx)
		}
		cert := hl.TLSCertFile
		if cert == "" && hl.TLSCertSecret != nil {
			cert = fmt.Sprintf("%s/%s/%s", tlsMountPath, hl.TLSCertSecret.Name, hl.TLSCertSecret.Key)
		}
		tlsCertFile.Add(cert, idx)
		key := hl.TLSKeyFile
		if key == "" && hl.TLSKeySecret != nil {
			key = fmt.Sprintf("%s/%s/%s", tlsMountPath, hl.TLSKeySecret.Name, hl.TLSKeySecret.Key)
		}
		tlsKeyFile.Add(key, idx)
		tlsMinVersion.Add(hl.TLSMinVersion, idx)
		autocertHosts.Add(hl.TLSAutocertHosts, idx)
		autocertEmail.Add(hl.TLSAutocertEmail, idx)
		autocertDir.Add(hl.TLSAutocertCacheDir, idx)
		if hl.MTLS != nil {
			mtls.Add(strconv.FormatBool(*hl.MTLS), idx)
		}
		ca := hl.MTLSCAFile
		if ca == "" && hl.MTLSCASecret != nil {
			ca = fmt.Sprintf("%s/%s/%s", tlsMountPath, hl.MTLSCASecret.Name, hl.MTLSCASecret.Key)
		}
		mtlsCAFile.Add(ca, idx)
		if hl.UseProxyProtocol != nil {
			proxyProto.Add(strconv.FormatBool(*hl.UseProxyProtocol), idx)
		}
	}
	return AppendFlagsToArgs(dst, len(listeners),
		listenAddr, tls, tlsCertFile, tlsKeyFile, tlsMinVersion,
		autocertHosts, autocertEmail, autocertDir, mtls, mtlsCAFile, proxyProto)
}

// AddHTTPListenerPortsTo appends container ports derived from HTTPListeners into dst.
func AddHTTPListenerPortsTo(dst []corev1.ContainerPort, listeners []vmv1beta1.HTTPListener) []corev1.ContainerPort {
	for _, hl := range listeners {
		_, portStr, err := net.SplitHostPort(hl.Addr)
		if err != nil {
			continue
		}
		portInt, err := strconv.ParseInt(portStr, 10, 32)
		if err != nil {
			continue
		}
		dst = append(dst, corev1.ContainerPort{
			Name:          hl.Name,
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: int32(portInt),
		})
	}
	return dst
}

// AddOTLPGRPCTLSConfigToVolumes adds OTLP gRPC tlsConfig volumes and mounts to the provided dsts
func AddOTLPGRPCTLSConfigToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, grpcSpec *vmv1.OTLPGRPCSpec, tlsMountPath string) ([]corev1.Volume, []corev1.VolumeMount) {
	if grpcSpec == nil || grpcSpec.TLSConfig == nil {
		return dstVolumes, dstMounts
	}

	tlsC := grpcSpec.TLSConfig
	if tlsC.CertSecret != nil {
		dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, tlsC.CertSecret, tlsMountPath)
	}
	if tlsC.KeySecret != nil {
		dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, tlsC.KeySecret, tlsMountPath)
	}
	return dstVolumes, dstMounts
}

// AddHTTPListenerTLSToVolumes mounts secret volumes for any TLS secrets referenced by HTTPListeners.
func AddHTTPListenerTLSToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, listeners []vmv1beta1.HTTPListener, tlsMountPath string) ([]corev1.Volume, []corev1.VolumeMount) {
	for _, hl := range listeners {
		if hl.TLSCertSecret != nil {
			dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, hl.TLSCertSecret, tlsMountPath)
		}
		if hl.TLSKeySecret != nil {
			dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, hl.TLSKeySecret, tlsMountPath)
		}
		if hl.MTLSCASecret != nil {
			dstVolumes, dstMounts = addTLSSecretVolume(dstVolumes, dstMounts, hl.MTLSCASecret, tlsMountPath)
		}
	}
	return dstVolumes, dstMounts
}

func StorageVolumeMountsTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, pvcSrc *corev1.PersistentVolumeClaimVolumeSource, storagePath, dataVolumeName string, isStatefulSet bool) ([]corev1.Volume, []corev1.VolumeMount, error) {
	foundMount := false
	for _, volumeMount := range mounts {
		rel, err := filepath.Rel(volumeMount.MountPath, storagePath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			if volumeMount.Name == dataVolumeName {
				foundMount = true
				break
			}
			return nil, nil, fmt.Errorf(
				"unexpected volume=%q mounted to path=%q, which is reserved for volume=%q, path=%q",
				volumeMount.Name, volumeMount.MountPath, dataVolumeName, storagePath)
		} else {
			if volumeMount.Name != dataVolumeName {
				continue
			}
			return nil, nil, fmt.Errorf(
				"unexpected volume=%q mounted to path=%q, expected path=%q",
				volumeMount.Name, volumeMount.MountPath, dataVolumeName)
		}
	}
	if !foundMount {
		mounts = append([]corev1.VolumeMount{{
			Name:      dataVolumeName,
			MountPath: storagePath,
		}}, mounts...)
	}

	for _, volume := range volumes {
		if volume.Name == dataVolumeName {
			if pvcSrc != nil {
				return nil, nil, fmt.Errorf("storage and %q volume are not allowed to be set together", dataVolumeName)
			}
			return volumes, mounts, nil
		}
	}
	if isStatefulSet {
		return volumes, mounts, nil
	}
	var source corev1.VolumeSource
	if pvcSrc != nil {
		source.PersistentVolumeClaim = pvcSrc
	} else {
		source.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}
	volumes = append([]corev1.Volume{{
		Name:         dataVolumeName,
		VolumeSource: source,
	}}, volumes...)
	return volumes, mounts, nil
}
