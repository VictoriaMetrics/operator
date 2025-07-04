package build

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// MustSkipRuntimeValidation defines whether runtime object validation must be skipped
// the most usual case for it, if webhook validation is configured
var MustSkipRuntimeValidation bool

// SetSkipRuntimeValidation configures MustSkipRuntimeValidation param
func SetSkipRuntimeValidation(mustSkip bool) {
	MustSkipRuntimeValidation = mustSkip
}

type builderOpts interface {
	client.Object
	PrefixedName() string
	AnnotationsFiltered() map[string]string
	AllLabels() map[string]string
	SelectorLabels() map[string]string
	AsOwner() []metav1.OwnerReference
	GetNamespace() string
	GetAdditionalService() *vmv1beta1.AdditionalServiceSpec
}

// PodDNSAddress formats pod dns address with optional domain name
func PodDNSAddress(baseName string, podIndex int32, namespace string, portName string, domain string) string {
	// The default DNS search path is .svc.<cluster domain>
	if domain == "" {
		return fmt.Sprintf("%s-%d.%s.%s:%s,", baseName, podIndex, baseName, namespace, portName)
	}
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

// LicenseArgsTo conditionally adds license commandline args into given args
func LicenseArgsTo(args []string, l *vmv1beta1.License, secretMountDir string) []string {
	return licenseArgsTo(args, l, secretMountDir, "-")
}

// LicenseDoubleDashArgsTo conditionally adds double-dash license commandline args into given args
func LicenseDoubleDashArgsTo(args []string, l *vmv1beta1.License, secretMountDir string) []string {
	return licenseArgsTo(args, l, secretMountDir, "--")
}

func licenseArgsTo(args []string, l *vmv1beta1.License, secretMountDir string, dashes string) []string {
	if l == nil || !l.IsProvided() {
		return args
	}
	if l.Key != nil {
		args = append(args, fmt.Sprintf("%slicense=%s", dashes, *l.Key))
	}
	if l.KeyRef != nil {
		args = append(args, fmt.Sprintf("%slicenseFile=%s", dashes, path.Join(secretMountDir, l.KeyRef.Name, l.KeyRef.Key)))
	}
	if l.ForceOffline != nil {
		args = append(args, fmt.Sprintf("%slicense.forceOffline=%v", dashes, *l.ForceOffline))
	}
	if l.ReloadInterval != nil {
		args = append(args, fmt.Sprintf("%slicenseFile.reloadInterval=%s", dashes, *l.ReloadInterval))
	}
	return args
}

// LicenseVolumeTo conditionally mounts secret with license key into given volumes and volume mounts
func LicenseVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, l *vmv1beta1.License, secretMountDir string) ([]corev1.Volume, []corev1.VolumeMount) {
	if l == nil || l.KeyRef == nil {
		return volumes, mounts
	}
	volumes = append(volumes, corev1.Volume{
		Name: "license",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: l.KeyRef.Name,
			},
		},
	})
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "license",
		ReadOnly:  true,
		MountPath: path.Join(secretMountDir, l.KeyRef.Name),
	})
	return volumes, mounts
}

func NewFlag(name, empty string) *Flag {
	return &Flag{
		name:  name,
		empty: empty,
	}
}

type Flag struct {
	isSet bool
	idx   int
	name  string
	value string
	empty string
}

func (f *Flag) Add(value string, idx int) {
	if idx > 0 {
		if len(f.value) == 0 {
			f.value += f.empty
		}
		f.value += ","
		f.value += strings.Repeat(f.empty+",", idx-f.idx-1)
		f.idx = idx
	}
	f.value += value
	if value != f.empty && !f.isSet {
		f.isSet = true
	}
}

func (f *Flag) IsSet() bool {
	return f.isSet
}

func AppendFlagsToArgs(args []string, total int, fss ...*Flag) []string {
	for _, f := range fss {
		if f.isSet {
			args = append(args, f.name+"="+f.value+strings.Repeat(","+f.empty, total-f.idx-1))
		}
	}
	return args
}

// StreamAggrArgsTo conditionally adds stream aggregation commandline args into given args
func StreamAggrArgsTo(args []string, prefix string, keys []string, cs ...*vmv1beta1.StreamAggrConfig) []string {
	if len(cs) == 0 {
		return args
	}

	configFlag := NewFlag(fmt.Sprintf("-%s.config", prefix), "")
	keepInputFlag := NewFlag(fmt.Sprintf("-%s.keepInput", prefix), "false")
	dropInputFlag := NewFlag(fmt.Sprintf("-%s.dropInput", prefix), "false")
	ignoreFirstIntervalsFlag := NewFlag(fmt.Sprintf("-%s.ignoreFirstIntervals", prefix), "0")
	ignoreOldSamplesFlag := NewFlag(fmt.Sprintf("-%s.ignoreOldSamples", prefix), "false")
	enableWindowsFlag := NewFlag(fmt.Sprintf("-%s.enableWindows", prefix), "false")
	dedupIntervalFlag := NewFlag(fmt.Sprintf("-%s.dedupInterval", prefix), "")
	dropInputLabelsFlag := NewFlag(fmt.Sprintf("-%s.dropInputLabels", prefix), "")

	for i, c := range cs {
		if c == nil {
			continue
		}
		if c.HasAnyRule() {
			configFlag.Add(path.Join(vmv1beta1.StreamAggrConfigDir, keys[i]), i)
			keepInputFlag.Add(strconv.FormatBool(c.KeepInput), i)
			dropInputFlag.Add(strconv.FormatBool(c.DropInput), i)
			ignoreFirstIntervalsFlag.Add(strconv.Itoa(c.IgnoreFirstIntervals), i)
			ignoreOldSamplesFlag.Add(strconv.FormatBool(c.IgnoreOldSamples), i)
			enableWindowsFlag.Add(strconv.FormatBool(c.EnableWindows), i)
		}
		// deduplication can work without stream aggregation rules
		dedupIntervalFlag.Add(c.DedupInterval, i)
		dropInputLabelsFlag.Add(strings.Join(c.DropInputLabels, ","), i)
	}
	args = AppendFlagsToArgs(args, len(cs), configFlag, keepInputFlag, dropInputFlag, ignoreFirstIntervalsFlag, ignoreOldSamplesFlag, enableWindowsFlag)
	args = AppendFlagsToArgs(args, len(cs), dedupIntervalFlag, dropInputLabelsFlag)
	return args
}

// StreamAggrVolumeTo conditionally mounts configmap with stream aggregation config into given volumes and volume mounts
func StreamAggrVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, name string, cs ...*vmv1beta1.StreamAggrConfig) ([]corev1.Volume, []corev1.VolumeMount) {
	if len(cs) == 0 {
		return volumes, mounts
	}
	hasRule := false
	for _, c := range cs {
		if c.HasAnyRule() {
			hasRule = true
			break
		}
	}
	if hasRule {
		volumes = append(volumes, corev1.Volume{
			Name: "stream-aggr-conf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: name,
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "stream-aggr-conf",
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		})
	}
	return volumes, mounts
}

// SyslogPortsTo adds syslog ports to pod spec
func SyslogPortsTo(dst []corev1.ContainerPort, syslogSpec *vmv1.SyslogServerSpec) []corev1.ContainerPort {
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

// SyslogServicePortsTo adds syslog ports to service spec
func SyslogServicePortsTo(svc *corev1.Service, syslogSpec *vmv1.SyslogServerSpec) {
	if syslogSpec == nil {
		return
	}
	for idx, tcp := range syslogSpec.TCPListeners {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       fmt.Sprintf("syslog-tcp-%d", idx),
			Protocol:   corev1.ProtocolTCP,
			Port:       tcp.ListenPort,
			TargetPort: intstr.FromInt32(tcp.ListenPort),
		})
	}

	for idx, udp := range syslogSpec.UDPListeners {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       fmt.Sprintf("syslog-udp-%d", idx),
			Protocol:   corev1.ProtocolUDP,
			Port:       udp.ListenPort,
			TargetPort: intstr.FromInt32(udp.ListenPort),
		})
	}
}

// SyslogArgsTo adds syslog command line flags
func SyslogArgsTo(args []string, syslogSpec *vmv1.SyslogServerSpec, ns string, ac *AssetsCache) ([]string, error) {
	if syslogSpec == nil {
		return args, nil
	}
	tcpListenAddr := NewFlag("-syslog.listenAddr.tcp", "")
	udpListenAddr := NewFlag("-syslog.listenAddr.udp", "")
	tcpStreamFields := NewFlag("-syslog.streamFields.tcp", `''`)
	udpStreamFields := NewFlag("-syslog.streamFields.udp", `''`)
	tcpIgnoreFields := NewFlag("-syslog.ignoreFields.tcp", `''`)
	udpIgnoreFields := NewFlag("-syslog.ignoreFields.udp", `''`)
	tcpDecolorizedFields := NewFlag("-syslog.decolorizeFields.tcp", `''`)
	udpDecolorizedFields := NewFlag("-syslog.decolorizeFields.udp", `''`)
	tcpTenantID := NewFlag("-syslog.tenantID.tcp", "''")
	udpTenantID := NewFlag("-syslog.tenantID.udp", "''")
	tcpCompress := NewFlag("-syslog.compressMethod.tcp", "''")
	udpCompress := NewFlag("-syslog.compressMethod.udp", "''")
	tlsEnabled := NewFlag("-syslog.tls", "")
	tlsCertFile := NewFlag("-syslog.tlsCertFile", "")
	tlsKeyFile := NewFlag("-syslog.tlsKeyFile", "")

	for i, sTCP := range syslogSpec.TCPListeners {
		tcpListenAddr.Add(fmt.Sprintf(":%d", sTCP.ListenPort), i)
		tcpStreamFields.Add(fmt.Sprintf("'%s'", string(sTCP.StreamFields)), i)
		tcpDecolorizedFields.Add(fmt.Sprintf("'%s'", string(sTCP.DecolorizeFields)), i)
		tcpIgnoreFields.Add(fmt.Sprintf("'%s'", string(sTCP.IgnoreFields)), i)
		tcpTenantID.Add(fmt.Sprintf("'%s'", sTCP.TenantID), i)
		tcpCompress.Add(fmt.Sprintf("'%s'", sTCP.CompressMethod), i)
		if sTCP.TLSConfig != nil {
			tlsEnabled.Add("true", i)
			creds, err := ac.BuildTLSServerCreds(ns, sTCP.TLSConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to get TLS credentials for Syslog: %w", err)
			}
			tlsCertFile.Add(creds.CertFile, i)
			tlsKeyFile.Add(creds.KeyFile, i)
		}
	}
	tcpLen := len(syslogSpec.TCPListeners)
	args = AppendFlagsToArgs(args, tcpLen, tcpListenAddr, tcpStreamFields, tcpIgnoreFields, tcpDecolorizedFields, tcpTenantID, tcpCompress, tlsEnabled, tlsCertFile, tlsKeyFile)

	for i, sUDP := range syslogSpec.UDPListeners {
		udpListenAddr.Add(fmt.Sprintf(":%d", sUDP.ListenPort), i)
		udpStreamFields.Add(fmt.Sprintf("'%s'", string(sUDP.StreamFields)), i)
		udpIgnoreFields.Add(fmt.Sprintf("'%s'", string(sUDP.IgnoreFields)), i)
		udpDecolorizedFields.Add(fmt.Sprintf("'%s'", string(sUDP.DecolorizeFields)), i)
		udpTenantID.Add(fmt.Sprintf("'%s'", sUDP.TenantID), i)
		udpCompress.Add(fmt.Sprintf("'%s'", sUDP.CompressMethod), i)
	}
	udpLen := len(syslogSpec.UDPListeners)
	args = AppendFlagsToArgs(args, udpLen, udpListenAddr, udpStreamFields, udpIgnoreFields, udpDecolorizedFields, udpTenantID, udpCompress)
	return args, nil
}
