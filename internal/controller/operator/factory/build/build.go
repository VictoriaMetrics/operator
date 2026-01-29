package build

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// disabledControllers defines set of disabled VM operator controllers
var disabledControllers map[string]struct{}

// SetDisabledControllers configures set of disabled controllers
func SetDisabledControllers(controllers map[string]struct{}) {
	disabledControllers = controllers
}

// IsControllerDisabled checks if controller for given kind is disabled
func IsControllerDisabled(kind string) bool {
	if len(disabledControllers) > 0 {
		_, ok := disabledControllers[kind]
		return ok
	}
	return false
}

// mustSkipRuntimeValidation defines whether runtime object validation must be skipped
// the most usual case for it, if webhook validation is configured
var mustSkipRuntimeValidation bool

// SetSkipRuntimeValidation configures mustSkipRuntimeValidation param
func SetSkipRuntimeValidation(mustSkip bool) {
	mustSkipRuntimeValidation = mustSkip
}

// MustSkipRuntimeValidation returns value of mustSkipRuntimeValidation param
func MustSkipRuntimeValidation() bool {
	return mustSkipRuntimeValidation
}

type builderOpts interface {
	client.Object
	PrefixedName() string
	FinalAnnotations() map[string]string
	FinalLabels() map[string]string
	SelectorLabels() map[string]string
	AsOwner() metav1.OwnerReference
	GetNamespace() string
	GetAdditionalService() *vmv1beta1.AdditionalServiceSpec
}

// PodDNSAddress formats pod dns address with optional domain name
func PodDNSAddress(baseName string, podIndex int32, namespace string, portName string, domain string) string {
	// The default DNS search path is .svc.<cluster domain>
	if domain == "" {
		return fmt.Sprintf("%s-%d.%s.%s:%s", baseName, podIndex, baseName, namespace, portName)
	}
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s", baseName, podIndex, baseName, namespace, domain, portName)
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

// NewEmptyFlag builds Flag with empty string default value
func NewEmptyFlag(name string) *Flag {
	return &Flag{
		name:  name,
		empty: "",
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
	ignoreFirstSampleIntervalFlag := NewFlag(fmt.Sprintf("-%s.ignoreFirstSampleInterval", prefix), "")
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
			ignoreFirstSampleIntervalFlag.Add(c.IgnoreFirstSampleInterval, i)
			ignoreOldSamplesFlag.Add(strconv.FormatBool(c.IgnoreOldSamples), i)
			enableWindowsFlag.Add(strconv.FormatBool(c.EnableWindows), i)
		}
		// deduplication can work without stream aggregation rules
		dedupIntervalFlag.Add(c.DedupInterval, i)
		dropInputLabelsFlag.Add(strings.Join(c.DropInputLabels, ","), i)
	}
	args = AppendFlagsToArgs(args, len(cs), configFlag, keepInputFlag, dropInputFlag, ignoreFirstIntervalsFlag, ignoreFirstSampleIntervalFlag, ignoreOldSamplesFlag, enableWindowsFlag)
	args = AppendFlagsToArgs(args, len(cs), dedupIntervalFlag, dropInputLabelsFlag)
	return args
}

type streamAggrOpts interface {
	builderOpts
	HasAnyStreamAggrRule() bool
}

// StreamAggrVolumeTo conditionally mounts configmap with stream aggregation config into given volumes and volume mounts
func StreamAggrVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, cr streamAggrOpts) ([]corev1.Volume, []corev1.VolumeMount) {
	if cr.HasAnyStreamAggrRule() {
		volumes = append(volumes, corev1.Volume{
			Name: string(StreamAggrConfigResourceKind),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ResourceName(StreamAggrConfigResourceKind, cr),
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      string(StreamAggrConfigResourceKind),
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		})
	}
	return volumes, mounts
}

// RelabelArgsTo conditionally adds relabel commandline args into given args
func RelabelArgsTo(args []string, flag string, keys []string, cs ...*vmv1beta1.CommonRelabelParams) []string {
	if len(cs) == 0 {
		return args
	}
	configFlag := NewFlag(fmt.Sprintf("-%s", flag), "")
	for i, c := range cs {
		if c == nil {
			continue
		}
		if c.HasAnyRelabellingConfigs() {
			configFlag.Add(path.Join(vmv1beta1.RelabelingConfigDir, keys[i]), i)
		}
	}
	return AppendFlagsToArgs(args, len(cs), configFlag)
}

type relabelOpts interface {
	builderOpts
	HasAnyRelabellingConfigs() bool
}

// RelabelVolumeTo conditionally mounts configmap with relabel config into given volumes and volume mounts
func RelabelVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, cr relabelOpts) ([]corev1.Volume, []corev1.VolumeMount) {
	if cr.HasAnyRelabellingConfigs() {
		volumes = append(volumes, corev1.Volume{
			Name: string(RelabelConfigResourceKind),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ResourceName(RelabelConfigResourceKind, cr),
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      string(RelabelConfigResourceKind),
			ReadOnly:  true,
			MountPath: vmv1beta1.RelabelingConfigDir,
		})
	}
	return volumes, mounts
}

// GzipConfig compresses data config
func GzipConfig(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write compressed data to buffer: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to flush compressed data to buffer: %w", err)
	}
	return buf.Bytes(), nil
}

// GunzipConfig uncompresses data config
func GunzipConfig(data []byte) ([]byte, error) {
	r := bytes.NewReader(data)
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to uncompress config: %w", err)
	}
	defer gr.Close()
	return io.ReadAll(gr)
}
