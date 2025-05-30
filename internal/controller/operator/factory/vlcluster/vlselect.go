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

func createOrUpdateVLSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLSelect == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVLSelectName(), cr.VLSelectSelectorLabels())

	if cr.Spec.VLSelect.PodDisruptionBudget != nil {
		pdb := build.PodDisruptionBudget(b, cr.Spec.VLSelect.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.VLSelect.PodDisruptionBudget != nil {
			prevB := newOptsBuilder(prevCR, prevCR.GetVLSelectName(), prevCR.VLSelectSelectorLabels())
			prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.VLSelect.PodDisruptionBudget)
		}
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVLSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	selectSvc, err := createOrUpdateVLSelectService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, false) {
		svs := build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VLSelect)
		if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
			// for backward compatibility we must keep job label value
			svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
		}
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VLSelect: %w", err)
		}
	}
	if err := createOrUpdateVLSelectDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func createOrUpdateVLSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLSelect.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVLSelectName(),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	b := newOptsBuilder(cr, cr.GetVLSelectName(), cr.VLSelectSelectorLabels())
	defaultHPA := build.HPA(b, targetRef, cr.Spec.VLSelect.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLSelect.HPA != nil {
		b := newOptsBuilder(prevCR, prevCR.GetVLSelectName(), prevCR.VLSelectSelectorLabels())
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VLSelect.HPA)
	}

	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)

}

func createOrUpdateVLSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VLSelect != nil {
		prevService = buildVLSelectService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VLSelect.ServiceSpec)
	}
	svc := buildVLSelectService(cr)
	if err := cr.Spec.VLSelect.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, s)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("VLSelect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile service for select: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile select service: %w", err)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.VLSelect != nil {
			prevPort = prevCR.Spec.VLSelect.Port
		}
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVLSelectName(), cr.Spec.VLSelect.Port, prevPort, "vlselect", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for select: %w", err)
		}
	}
	return svc, nil
}

func buildVLSelectService(cr *vmv1.VLCluster) *corev1.Service {
	b := &optsBuilder{
		cr,
		cr.GetVLSelectName(),
		cr.FinalLabels(cr.VLSelectSelectorLabels()),
		cr.VLSelectSelectorLabels(),
		cr.Spec.VLSelect.ServiceSpec,
	}
	svc := build.Service(b, cr.Spec.VLSelect.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svc.Name = cr.GetVLSelectLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVLSelectName()
	}

	return svc

}

func createOrUpdateVLSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.VLSelect != nil {
		var err error
		prevDep, err = buildVLSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select spec: %w", err)
		}
	}
	newDep, err := buildVLSelectDeployment(cr)
	if err != nil {
		return err
	}

	return reconcile.Deployment(ctx, rclient, newDep, prevDep, cr.Spec.VLSelect.HPA != nil)
}

func buildVLSelectDeployment(cr *vmv1.VLCluster) (*appsv1.Deployment, error) {
	podSpec, err := buildVLSelectPodSpec(cr)
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VLSelect.UpdateStrategy != nil {
		strategyType = *cr.Spec.VLSelect.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVLSelectName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VLSelectSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.VLSelect.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VLSelectSelectorLabels(),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.VLSelect.UseStrictSecurity, false), &cr.Spec.VLSelect.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func buildVLSelectPodSpec(cr *vmv1.VLCluster) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLSelect.Port),
		"-internalinsert.disable=true",
	}
	if cr.Spec.VLSelect.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VLSelect.LogLevel))
	}
	if cr.Spec.VLSelect.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VLSelect.LogFormat))
	}

	if cr.Spec.VLStorage != nil && cr.Spec.VLStorage.ReplicaCount != nil {
		// TODO: check TLS
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("select") {
			storageArg += build.PodDNSAddress(cr.GetVLStorageName(), i, cr.Namespace, cr.Spec.VLStorage.Port, cr.Spec.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		args = append(args, storageArg)

	}

	if len(cr.Spec.VLSelect.ExtraEnvs) > 0 || len(cr.Spec.VLSelect.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.VLSelect.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{
		Name:          "http",
		Protocol:      "TCP",
		ContainerPort: intstr.Parse(cr.Spec.VLSelect.Port).IntVal,
	})

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.VLSelect.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.VLSelect.VolumeMounts...)

	for _, s := range cr.Spec.VLSelect.Secrets {
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

	for _, c := range cr.Spec.VLSelect.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VLSelect.ExtraArgs, "-")
	sort.Strings(args)
	selectContainers := corev1.Container{
		Name:                     "vlselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VLSelect.Image.Repository, cr.Spec.VLSelect.Image.Tag),
		ImagePullPolicy:          cr.Spec.VLSelect.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VLSelect.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.VLSelect.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	selectContainers = build.Probe(selectContainers, cr.Spec.VLSelect)
	operatorContainers := []corev1.Container{selectContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLSelect.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VLSelect.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VLSelect.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VLSelect.TopologySpreadConstraints {
		if cr.Spec.VLSelect.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLSelect.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VLSelectSelectorLabels(),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VLSelectPodLabels(),
			Annotations: cr.VLSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.VLSelect.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			RestartPolicy:      "Always",
		},
	}

	return podSpec, nil
}
