package vtcluster

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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func createOrUpdateVTInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert == nil {
		return nil
	}

	if err := createOrUpdatePodDisruptionBudgetForVTInsert(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTInsertDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	insertSvc, err := createOrUpdateVTInsertService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if err := createOrUpdateVTInsertHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, false) {
		svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.Insert)
		if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
			// for backward compatibility we must keep job label value
			svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
		}
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTInsert: %w", err)
		}
	}

	return nil
}

func createOrUpdatePodDisruptionBudgetForVTInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert.PodDisruptionBudget == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVTInsertName(), cr.VTInsertSelectorLabels())
	pdb := build.PodDisruptionBudget(b, cr.Spec.Insert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.Insert.PodDisruptionBudget != nil {
		prevB := newOptsBuilder(prevCR, prevCR.GetVTInsertName(), prevCR.VTInsertSelectorLabels())
		prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.Insert.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVTInsertDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevDeploy *appsv1.Deployment

	if prevCR != nil && prevCR.Spec.Insert != nil {
		var err error
		prevDeploy, err = buildVTInsertDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeployment, err := buildVTInsertDeployment(cr)
	if err != nil {
		return err
	}
	return reconcile.Deployment(ctx, rclient, newDeployment, prevDeploy, cr.Spec.Insert.HPA != nil)
}

func buildVTInsertDeployment(cr *vmv1.VTCluster) (*appsv1.Deployment, error) {

	podSpec, err := buildVTInsertPodSpec(cr)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.Insert.UpdateStrategy != nil {
		strategyType = *cr.Spec.Insert.UpdateStrategy
	}
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVTInsertName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VTInsertSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             cr.Spec.Insert.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.Insert.RevisionHistoryLimitCount,
			MinReadySeconds:      cr.Spec.Insert.MinReadySeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.Insert.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VTInsertSelectorLabels(),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.Insert.UseStrictSecurity, false), &cr.Spec.Insert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func buildVTInsertPodSpec(cr *vmv1.VTCluster) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Insert.Port),
		"-internalselect.disable=true",
	}
	if cr.Spec.Insert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.Insert.LogLevel))
	}
	if cr.Spec.Insert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.Insert.LogFormat))
	}

	if cr.Spec.Storage != nil && cr.Spec.Storage.ReplicaCount != nil {
		// TODO: check TLS
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("insert")
		for idx, i := range storageNodeIds {
			storageNodeFlag.Add(build.PodDNSAddress(cr.GetVTStorageName(), i, cr.Namespace, cr.Spec.Storage.Port, cr.Spec.ClusterDomainName), idx)
		}
		totalNodes := len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}

	if len(cr.Spec.Insert.ExtraEnvs) > 0 || len(cr.Spec.Insert.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.Insert.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.Insert.Port).IntVal,
		},
	}

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.Insert.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.Insert.VolumeMounts...)

	for _, s := range cr.Spec.Insert.Secrets {
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

	for _, c := range cr.Spec.Insert.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.Insert.ExtraArgs, "-")
	sort.Strings(args)

	insertContainers := corev1.Container{
		Name:                     "vtinsert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Insert.Image.Repository, cr.Spec.Insert.Image.Tag),
		ImagePullPolicy:          cr.Spec.Insert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Insert.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.Insert.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	insertContainers = build.Probe(insertContainers, cr.Spec.Insert)
	operatorContainers := []corev1.Container{insertContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.Insert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.Insert.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Insert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.Insert.TopologySpreadConstraints {
		if cr.Spec.Insert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.Insert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VTInsertSelectorLabels(),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VTInsertPodLabels(),
			Annotations: cr.VTInsertPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.Insert.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return podSpec, nil
}

func createOrUpdateVTInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVTInsertName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	t := newOptsBuilder(cr, cr.GetVTInsertName(), cr.VTInsertSelectorLabels())
	newHPA := build.HPA(t, targetRef, cr.Spec.Insert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Insert.HPA != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVTInsertName(), prevCR.VTInsertSelectorLabels())
		prevHPA = build.HPA(t, targetRef, prevCR.Spec.Insert.HPA)
	}
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA)
}

func createOrUpdateVTInsertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) (*corev1.Service, error) {
	newService := buildVTInsertService(cr)
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.Insert != nil {
		prevService = buildVTInsertService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.Insert.ServiceSpec)
	}
	if err := cr.Spec.Insert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("VTInsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
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
		if prevCR != nil && prevCR.Spec.Insert != nil {
			prevPort = prevCR.Spec.Insert.Port
		}
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVTInsertName(), cr.Spec.Insert.Port, prevPort, "vtinsert", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for insert: %w", err)
		}
	}

	return newService, nil
}

func buildVTInsertService(cr *vmv1.VTCluster) *corev1.Service {
	t := &optsBuilder{
		cr,
		cr.GetVTInsertName(),
		cr.FinalLabels(cr.VTInsertSelectorLabels()),
		cr.VTInsertSelectorLabels(),
		cr.Spec.Insert.ServiceSpec,
	}

	svc := build.Service(t, cr.Spec.Insert.Port, nil)
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svc.Name = cr.GetVTInsertLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVTInsertName()
	}
	return svc
}
