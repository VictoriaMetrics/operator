package vlcluster

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func createOrUpdateVLInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLInsert == nil {
		return nil
	}

	if err := createOrUpdatePodDisruptionBudgetForVLInsert(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLInsertDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	insertSvc, err := createOrUpdateVLInsertService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if err := createOrUpdateVLInsertHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.VLInsert.DisableSelfServiceScrape, false) {
		svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VLInsert)
		if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
			// for backward compatibility we must keep job label value
			svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
		}
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VLInsert: %w", err)
		}
	}

	return nil
}

func createOrUpdatePodDisruptionBudgetForVLInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLInsert.PodDisruptionBudget == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVLInsertName(), cr.VLInsertSelectorLabels())
	pdb := build.PodDisruptionBudget(b, cr.Spec.VLInsert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VLInsert.PodDisruptionBudget != nil {
		prevB := newOptsBuilder(prevCR, prevCR.GetVLInsertName(), prevCR.VLInsertSelectorLabels())
		prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.VLInsert.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVLInsertDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevDeploy *appsv1.Deployment

	if prevCR != nil && prevCR.Spec.VLInsert != nil {
		var err error
		prevDeploy, err = buildVLInsertDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeployment, err := buildVLInsertDeployment(cr)
	if err != nil {
		return err
	}
	return reconcile.Deployment(ctx, rclient, newDeployment, prevDeploy, cr.Spec.VLInsert.HPA != nil)
}

func buildVLInsertDeployment(cr *vmv1.VLCluster) (*appsv1.Deployment, error) {

	podSpec, err := buildVLInsertPodSpec(cr)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VLInsert.UpdateStrategy != nil {
		strategyType = *cr.Spec.VLInsert.UpdateStrategy
	}
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVLInsertName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VLInsertSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             cr.Spec.VLInsert.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.VLInsert.RevisionHistoryLimitCount,
			MinReadySeconds:      cr.Spec.VLInsert.MinReadySeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.VLInsert.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VLInsertSelectorLabels(),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VLInsert.UseStrictSecurity, false), &cr.Spec.VLInsert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func buildVLInsertPodSpec(cr *vmv1.VLCluster) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLInsert.Port),
		"-internalselect.disable=true",
	}
	if cr.Spec.VLInsert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VLInsert.LogLevel))
	}
	if cr.Spec.VLInsert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VLInsert.LogFormat))
	}

	if cr.Spec.VLStorage != nil && cr.Spec.VLStorage.ReplicaCount != nil {
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("insert") {
			// TODO: introduce TLS webserver config for storage nodes
			storageArg += build.PodDNSAddress(cr.GetVLStorageName(), i, cr.Namespace, cr.Spec.VLStorage.Port, cr.Spec.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		args = append(args, storageArg)
	}
	if len(cr.Spec.VLInsert.ExtraEnvs) > 0 || len(cr.Spec.VLInsert.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.VLInsert.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VLInsert.Port).IntVal,
		},
	}

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.VLInsert.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.VLInsert.VolumeMounts...)

	if cr.Spec.VLInsert.SyslogSpec != nil && !cr.Spec.RequestsLoadBalancer.Enabled {
		ports = addSyslogPortsTo(ports, cr.Spec.VLInsert.SyslogSpec)
		args = buildSyslogArgs(args, cr.Spec.VLInsert.SyslogSpec)
		for _, tcpC := range cr.Spec.VLInsert.SyslogSpec.TCPListeners {
			volumes = addTLSConfigToVolumes(volumes, tcpC.TLSConfig)
			vmMounts = addTLSConfigToVolumeMounts(vmMounts, tcpC.TLSConfig)
		}
	}

	for _, s := range cr.Spec.VLInsert.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VLInsert.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VLInsert.ExtraArgs, "-")
	sort.Strings(args)

	insertContainers := corev1.Container{
		Name:                     "vlinsert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VLInsert.Image.Repository, cr.Spec.VLInsert.Image.Tag),
		ImagePullPolicy:          cr.Spec.VLInsert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VLInsert.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.VLInsert.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	insertContainers = build.Probe(insertContainers, cr.Spec.VLInsert)
	operatorContainers := []corev1.Container{insertContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLInsert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VLInsert.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VLInsert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VLInsert.TopologySpreadConstraints {
		if cr.Spec.VLInsert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLInsert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VLInsertSelectorLabels(),
			}
		}
	}

	var subdomain string
	if cr.Spec.VLInsert.ForceSubdomain {
		subdomain = cr.GetVLInsertLBName()
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VLInsertPodLabels(),
			Annotations: cr.VLInsertPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.VLInsert.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			Subdomain:          subdomain,
		},
	}

	return podSpec, nil
}

func createOrUpdateVLInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLInsert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVLInsertName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	t := newOptsBuilder(cr, cr.GetVLInsertName(), cr.VLInsertSelectorLabels())
	newHPA := build.HPA(t, targetRef, cr.Spec.VLInsert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLInsert.HPA != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVLInsertName(), prevCR.VLInsertSelectorLabels())
		prevHPA = build.HPA(t, targetRef, prevCR.Spec.VLInsert.HPA)
	}
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA)
}

func createOrUpdateVLInsertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) (*corev1.Service, error) {
	newService := buildVLInsertService(cr)
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VLInsert != nil {
		prevService = buildVLInsertService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VLInsert.ServiceSpec)
	}
	if err := cr.Spec.VLInsert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("VLInsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile insert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile insert service: %w", err)
	}

	// create extra service for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.VLInsert != nil {
			prevPort = prevCR.Spec.VLInsert.Port
		}
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVLInsertName(), cr.Spec.VLInsert.Port, prevPort, "vlinsert", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for insert: %w", err)
		}
	}

	return newService, nil
}

func buildVLInsertService(cr *vmv1.VLCluster) *corev1.Service {
	t := &optsBuilder{
		cr,
		cr.GetVLInsertName(),
		cr.FinalLabels(cr.VLInsertSelectorLabels()),
		cr.VLInsertSelectorLabels(),
		cr.Spec.VLInsert.ServiceSpec,
	}

	svc := build.Service(t, cr.Spec.VLInsert.Port, func(svc *corev1.Service) {
		syslogSpec := cr.Spec.VLInsert.SyslogSpec
		if syslogSpec == nil || cr.Spec.RequestsLoadBalancer.Enabled {
			// fast path
			return
		}
		addSyslogPortsToService(svc, syslogSpec)
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svc.Name = cr.GetVLInsertLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVLInsertName()
	}
	return svc
}

func buildSyslogArgs(dst []string, syslogSpec *vmv1.SyslogServerSpec) []string {
	if syslogSpec == nil {
		return dst
	}
	type groupFlag struct {
		isNotNull   bool
		flagSetting string
	}

	tcpListenAddr := groupFlag{flagSetting: "-syslog.listenAddr.tcp="}
	udpListenAddr := groupFlag{flagSetting: "-syslog.listenAddr.udp="}
	tcpStreamFields := groupFlag{flagSetting: "-syslog.streamFields.tcp="}
	udpStreamFileds := groupFlag{flagSetting: "-syslog.streamFields.udp="}
	tcpIgnoreFields := groupFlag{flagSetting: "-syslog.ignoreFields.tcp="}
	udpIgnoreFields := groupFlag{flagSetting: "-syslog.ignoreFields.udp="}
	tcpDecolorizedFields := groupFlag{flagSetting: "-syslog.decolorizeFields.tcp="}
	udpDecolorizedFields := groupFlag{flagSetting: "-syslog.decolorizeFields.udp="}
	tcpTenantID := groupFlag{flagSetting: "-syslog.tenantID.tcp="}
	udpTenantID := groupFlag{flagSetting: "-syslog.tenantID.udp="}
	tcpCompress := groupFlag{flagSetting: "-syslog.compressMethod.tcp="}
	udpCompress := groupFlag{flagSetting: "-syslog.compressMethod.udp="}

	tlsEnabled := groupFlag{flagSetting: "-syslog.tls="}
	tlsCertFile := groupFlag{flagSetting: "-syslog.tlsCertFile="}
	tlsKeyFile := groupFlag{flagSetting: "-syslog.tlsKeyFile="}

	var value string
	addValue := func(flag *groupFlag, fieldValue string) {
		value = ""
		if len(fieldValue) > 0 {
			flag.isNotNull = true
			value = fieldValue
		}
		flag.flagSetting += fmt.Sprintf("'%s',", value)
	}

	for _, sTCP := range syslogSpec.TCPListeners {
		tcpListenAddr.flagSetting += fmt.Sprintf(":%d,", sTCP.ListenPort)
		tcpListenAddr.isNotNull = true
		addValue(&tcpStreamFields, string(sTCP.StreamFields))
		addValue(&tcpDecolorizedFields, string(sTCP.DecolorizeFields))
		addValue(&tcpIgnoreFields, string(sTCP.IgnoreFields))
		addValue(&tcpTenantID, sTCP.TenantID)
		addValue(&tcpCompress, sTCP.CompressMethod)

		value = ""
		if sTCP.TLSConfig != nil {
			tlsEnabled.isNotNull = true
			value = "true"
		}
		tlsEnabled.flagSetting += fmt.Sprintf("%s,", value)

		if sTCP.TLSConfig != nil {
			tlsC := sTCP.TLSConfig
			value = ""
			switch {
			case tlsC.CertFile != "":
				value = tlsC.CertFile
				tlsCertFile.isNotNull = true
			case tlsC.CertSecret != nil:
				tlsCertFile.isNotNull = true
				value = fmt.Sprintf("%s/%s/%s", tlsServerConfigMountPath, tlsC.CertSecret.Name, tlsC.CertSecret.Key)
			}
			tlsCertFile.flagSetting += fmt.Sprintf("%s,", value)

			value = ""
			switch {
			case tlsC.KeyFile != "":
				value = tlsC.KeyFile
				tlsKeyFile.isNotNull = true
			case tlsC.KeySecret != nil:
				value = fmt.Sprintf("%s/%s/%s", tlsServerConfigMountPath, tlsC.KeySecret.Name, tlsC.KeySecret.Key)
				tlsKeyFile.isNotNull = true
			}
			tlsKeyFile.flagSetting += fmt.Sprintf("%s,", value)
		}
	}

	for _, sUDP := range syslogSpec.UDPListeners {
		udpListenAddr.flagSetting += fmt.Sprintf(":%d,", sUDP.ListenPort)
		udpListenAddr.isNotNull = true
		addValue(&udpStreamFileds, string(sUDP.StreamFields))
		addValue(&udpIgnoreFields, string(sUDP.IgnoreFields))
		addValue(&udpDecolorizedFields, string(sUDP.DecolorizeFields))
		addValue(&udpTenantID, sUDP.TenantID)
		addValue(&udpCompress, sUDP.CompressMethod)
	}
	flags := []groupFlag{
		tcpListenAddr,
		tcpStreamFields,
		tcpIgnoreFields,
		tcpDecolorizedFields,
		tcpTenantID,
		tcpCompress,
		tlsEnabled,
		tlsCertFile,
		tlsKeyFile,

		udpListenAddr,
		udpStreamFileds,
		udpIgnoreFields,
		udpDecolorizedFields,
		udpTenantID,
		udpCompress,
	}
	for _, flagArg := range flags {
		if flagArg.isNotNull {
			dst = append(dst, strings.TrimSuffix(flagArg.flagSetting, ","))
		}
	}
	return dst
}

func addSyslogPortsTo(dst []corev1.ContainerPort, syslogSpec *vmv1.SyslogServerSpec) []corev1.ContainerPort {
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

func addSyslogPortsToService(svc *corev1.Service, syslogSpec *vmv1.SyslogServerSpec) {
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
		},
		)
	}
}
