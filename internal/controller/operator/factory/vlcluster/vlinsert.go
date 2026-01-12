package vlcluster

import (
	"context"
	"fmt"
	"path"
	"sort"

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
	"github.com/VictoriaMetrics/operator/internal/config"
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
	cfg := config.MustGetBaseConfig()
	if !ptr.Deref(cr.Spec.VLInsert.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {
		svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VLInsert)
		if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
			// for backward compatibility we must keep job label value
			svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
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
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	pdb := build.PodDisruptionBudget(b, cr.Spec.VLInsert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VLInsert.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VLInsert.PodDisruptionBudget)
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
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentInsert),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			},
			Template: *podSpec,
		},
	}
	cfg := config.MustGetBaseConfig()
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VLInsert.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.VLInsert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func buildVLInsertPodSpec(cr *vmv1.VLCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLInsert.Port),
		"-internalselect.disable=true",
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.VLInsert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VLInsert.LogLevel))
	}
	if cr.Spec.VLInsert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VLInsert.LogFormat))
	}

	if cr.Spec.VLStorage != nil && cr.Spec.VLStorage.ReplicaCount != nil {
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("insert")
		for idx, i := range storageNodeIds {
			// TODO: introduce TLS webserver config for storage nodes
			storageNodeFlag.Add(build.PodDNSAddress(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), i, cr.Namespace, cr.Spec.VLStorage.Port, cr.Spec.ClusterDomainName), idx)
		}
		totalNodes := len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
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
		ports = build.AddSyslogPortsTo(ports, cr.Spec.VLInsert.SyslogSpec)
		args = build.AddSyslogArgsTo(args, cr.Spec.VLInsert.SyslogSpec, tlsServerConfigMountPath)
		volumes, vmMounts = build.AddSyslogTLSConfigToVolumes(volumes, vmMounts, cr.Spec.VLInsert.SyslogSpec, tlsServerConfigMountPath)
	}

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

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

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLInsert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VLInsert.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VLInsert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VLInsert.TopologySpreadConstraints {
		if cr.Spec.VLInsert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLInsert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentInsert),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentInsert),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.VLInsert.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return podSpec, nil
}

func createOrUpdateVLInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLInsert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	newHPA := build.HPA(b, targetRef, cr.Spec.VLInsert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLInsert.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VLInsert.HPA)
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
		kind := vmv1beta1.ClusterComponentInsert
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.VLInsert.Port, prevPort); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for insert: %w", err)
		}
	}

	return newService, nil
}

func buildVLInsertService(cr *vmv1.VLCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	svc := build.Service(b, cr.Spec.VLInsert.Port, func(svc *corev1.Service) {
		syslogSpec := cr.Spec.VLInsert.SyslogSpec
		if syslogSpec == nil || cr.Spec.RequestsLoadBalancer.Enabled {
			// fast path
			return
		}
		build.AddSyslogPortsToService(svc, syslogSpec)
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svc.Name = cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmv1beta1.VMAuthLBServiceProxyJobNameLabel] = cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
	}
	return svc
}
