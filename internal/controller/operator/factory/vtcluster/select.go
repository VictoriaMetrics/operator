package vtcluster

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

func createOrUpdateVTSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Select == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVTSelectName(), cr.VTSelectSelectorLabels())

	if cr.Spec.Select.PodDisruptionBudget != nil {
		pdb := build.PodDisruptionBudget(b, cr.Spec.Select.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.Select.PodDisruptionBudget != nil {
			prevB := newOptsBuilder(prevCR, prevCR.GetVTSelectName(), prevCR.VTSelectSelectorLabels())
			prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.Select.PodDisruptionBudget)
		}
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVTSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	selectSvc, err := createOrUpdateVTSelectService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		svs := build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.Select)
		if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
			// for backward compatibility we must keep job label value
			svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
		}
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTSelect: %w", err)
		}
	}
	if err := createOrUpdateVTSelectDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func createOrUpdateVTSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Select.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVTSelectName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := newOptsBuilder(cr, cr.GetVTSelectName(), cr.VTSelectSelectorLabels())
	defaultHPA := build.HPA(b, targetRef, cr.Spec.Select.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Select.HPA != nil {
		b := newOptsBuilder(prevCR, prevCR.GetVTSelectName(), prevCR.VTSelectSelectorLabels())
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Select.HPA)
	}

	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)

}

func createOrUpdateVTSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.Select != nil {
		prevService = buildVTSelectService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.Select.ServiceSpec)
	}
	svc := buildVTSelectService(cr)
	if err := cr.Spec.Select.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, s)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("VTSelect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, svc.Name)
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
		if prevCR != nil && prevCR.Spec.Select != nil {
			prevPort = prevCR.Spec.Select.Port
		}
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVTSelectName(), cr.Spec.Select.Port, prevPort, "vtselect", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for select: %w", err)
		}
	}
	return svc, nil
}

func buildVTSelectService(cr *vmv1.VTCluster) *corev1.Service {
	b := &optsBuilder{
		cr,
		cr.GetVTSelectName(),
		cr.FinalLabels(cr.VTSelectSelectorLabels()),
		cr.VTSelectSelectorLabels(),
		cr.Spec.Select.ServiceSpec,
	}
	svc := build.Service(b, cr.Spec.Select.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svc.Name = cr.GetVTSelectLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVTSelectName()
	}

	return svc

}

func createOrUpdateVTSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.Select != nil {
		var err error
		prevDep, err = buildVTSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select spec: %w", err)
		}
	}
	newDep, err := buildVTSelectDeployment(cr)
	if err != nil {
		return err
	}

	return reconcile.Deployment(ctx, rclient, newDep, prevDep, cr.Spec.Select.HPA != nil)
}

func buildVTSelectDeployment(cr *vmv1.VTCluster) (*appsv1.Deployment, error) {
	podSpec, err := buildVTSelectPodSpec(cr)
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.Select.UpdateStrategy != nil {
		strategyType = *cr.Spec.Select.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVTSelectName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VTSelectSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.Select.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VTSelectSelectorLabels(),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.Select.UseStrictSecurity, false), &cr.Spec.Select.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func buildVTSelectPodSpec(cr *vmv1.VTCluster) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Select.Port),
		"-internalinsert.disable=true",
	}
	if cr.Spec.Select.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.Select.LogLevel))
	}
	if cr.Spec.Select.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.Select.LogFormat))
	}

	if cr.Spec.Storage != nil && cr.Spec.Storage.ReplicaCount != nil {
		// TODO: check TLS
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("select") {
			storageArg += build.PodDNSAddress(cr.GetVTStorageName(), i, cr.Namespace, cr.Spec.Storage.Port, cr.Spec.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		args = append(args, storageArg)

	}

	if len(cr.Spec.Select.ExtraEnvs) > 0 || len(cr.Spec.Select.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.Select.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{
		Name:          "http",
		Protocol:      "TCP",
		ContainerPort: intstr.Parse(cr.Spec.Select.Port).IntVal,
	})

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.Select.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.Select.VolumeMounts...)

	for _, s := range cr.Spec.Select.Secrets {
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

	for _, c := range cr.Spec.Select.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.Select.ExtraArgs, "-")
	sort.Strings(args)
	selectContainers := corev1.Container{
		Name:                     "vtselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Select.Image.Repository, cr.Spec.Select.Image.Tag),
		ImagePullPolicy:          cr.Spec.Select.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Select.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.Select.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	selectContainers = build.Probe(selectContainers, cr.Spec.Select)
	operatorContainers := []corev1.Container{selectContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.Select.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.Select.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Select.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.Select.TopologySpreadConstraints {
		if cr.Spec.Select.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.Select.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VTSelectSelectorLabels(),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VTSelectPodLabels(),
			Annotations: cr.VTSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.Select.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			RestartPolicy:      "Always",
		},
	}

	return podSpec, nil
}
