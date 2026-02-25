package build

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

const probeTimeoutSeconds int32 = 5
const DataVolumeName = "data"

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

const (
	// DefaultTerminationGracePeriodSeconds is the default termination grace period for all components
	DefaultTerminationGracePeriodSeconds int64 = 30
)

// AddHTTPShutdownDelayArg adds default -http.shutdownDelay flag if user didn't override it in extraArgs.
// The delay is derived from terminationGracePeriodSeconds.
// It is added only for new resources
func AddHTTPShutdownDelayArg(args []string, extraArgs map[string]string, terminationGracePeriodSeconds *int64, isNewResource bool) []string {
	if _, ok := extraArgs["http.shutdownDelay"]; ok {
		return args
	}

	if !isNewResource {
		return args
	}

	var delaySeconds int64
	if terminationGracePeriodSeconds != nil {
		delaySeconds = *terminationGracePeriodSeconds
	} else {
		delaySeconds = DefaultTerminationGracePeriodSeconds
	}

	args = append(args, fmt.Sprintf("-http.shutdownDelay=%ds", delaySeconds))
	return args
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
	} else {
		containerImage = "library/" + containerImage
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
func AddSyslogArgsTo(dst []string, syslogSpec *vmv1.SyslogServerSpec, tlsServerConfigMountPath string) []string {
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

	var value string

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
				value = fmt.Sprintf("%s/%s/%s", tlsServerConfigMountPath, tlsC.CertSecret.Name, tlsC.CertSecret.Key)
			}
			tlsCertFile.Add(value, idx)
			value = ""
			switch {
			case tlsC.KeyFile != "":
				value = tlsC.KeyFile
			case tlsC.KeySecret != nil:
				value = fmt.Sprintf("%s/%s/%s", tlsServerConfigMountPath, tlsC.KeySecret.Name, tlsC.KeySecret.Key)
			}
			tlsKeyFile.Add(value, idx)
		}
	}
	dst = AppendFlagsToArgs(dst, len(syslogSpec.TCPListeners), tcpListenAddr, tcpStreamFields, tcpIgnoreFields, tcpDecolorizedFields, tcpTenantID, tcpCompress, tlsEnabled, tlsCertFile, tlsKeyFile)

	udpListenAddr := NewEmptyFlag("-syslog.listenAddr.udp")
	// vmv1.FieldsListString must be quoted with ''
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

// AddSyslogTLSConfigToVolumes adds syslog tlsConfig volumes and mounts to the provided dsts
func AddSyslogTLSConfigToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, syslogSpec *vmv1.SyslogServerSpec, tlsServerConfigMountPath string) ([]corev1.Volume, []corev1.VolumeMount) {
	if syslogSpec == nil || len(syslogSpec.TCPListeners) == 0 {
		return dstVolumes, dstMounts
	}

	addSecretVolume := func(sr *corev1.SecretKeySelector) {
		name := fmt.Sprintf("secret-tls-%s", sr.Name)
		for _, dst := range dstVolumes {
			if dst.Name == name {
				return
			}
		}
		dstVolumes = append(dstVolumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sr.Name,
				},
			},
		})
	}
	addSecretMount := func(sr *corev1.SecretKeySelector) {
		name := fmt.Sprintf("secret-tls-%s", sr.Name)
		for _, dst := range dstMounts {
			if dst.Name == name {
				return
			}
		}
		dstMounts = append(dstMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: fmt.Sprintf("%s/%s", tlsServerConfigMountPath, sr.Name),
		})
	}
	for _, tc := range syslogSpec.TCPListeners {
		if tc.TLSConfig == nil {
			continue
		}
		tlsC := tc.TLSConfig
		switch {
		case tlsC.CertFile != "":
		case tlsC.CertSecret != nil:
			addSecretVolume(tlsC.CertSecret)
			addSecretMount(tlsC.CertSecret)
		}

		switch {
		case tlsC.KeyFile != "":
		case tlsC.KeySecret != nil:
			addSecretVolume(tlsC.KeySecret)
			addSecretMount(tlsC.KeySecret)
		}

	}
	return dstVolumes, dstMounts
}

func StorageVolumeMountsTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, pvcSrc *corev1.PersistentVolumeClaimVolumeSource, storagePath, dataVolumeName string) ([]corev1.Volume, []corev1.VolumeMount, error) {
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
