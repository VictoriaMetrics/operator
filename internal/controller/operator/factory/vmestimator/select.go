package vmestimator

import (
	"context"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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

func createOrUpdateSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	if cr.Spec.Select == nil {
		return nil
	}
	// select could be added to the existing object, there are no previous select objects in this case
	if prevCR != nil && prevCR.Spec.Select == nil {
		prevCR = nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	owner := cr.AsOwner()
	var prev componentObjects
	var prevB *build.ChildBuilder
	if prevCR != nil {
		ps := prevCR.Spec.Select
		prev = componentObjects{pdb: ps.PodDisruptionBudget, np: ps.NetworkPolicy, vpa: ps.VPA}
		prevB = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)

	}
	s := cr.Spec.Select
	cur := componentObjects{pdb: s.PodDisruptionBudget, np: s.NetworkPolicy, vpa: s.VPA}
	if err := createOrUpdateComponentObjects(ctx, rclient, b, prevB, &owner, "Deployment", cur, prev); err != nil {
		return err
	}
	if err := createOrUpdateSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateSelectService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateSelectDeployment(ctx, rclient, cr, prevCR)
}

func createOrUpdateSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	if cr.Spec.Select.HPA == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newHPA := build.HPA(b, targetRef, cr.Spec.Select.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Select.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Select.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA, &owner)
}

func buildSelectScrape(cr *vmv1.VMEstimator, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Select == nil || ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Select)
}

func buildSelectService(cr *vmv1.VMEstimator) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	return build.Service(b, cr.Spec.Select.Port, nil)
}

func createOrUpdateSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = buildSelectService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Select.ServiceSpec)
	}
	svc := buildSelectService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Select.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("select additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile select additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile select service: %w", err)
	}
	if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		svs := buildSelectScrape(cr, svc)
		prevSvs := buildSelectScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for select: %w", err)
		}
	}
	return nil
}

func createOrUpdateSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil {
		var err error
		prevDep, err = buildSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select deployment: %w", err)
		}
	}
	newDep, err := buildSelectDeployment(cr)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	o := reconcile.DeploymentOpts{
		PatchSpec: func(existingSpec, newSpec *appsv1.DeploymentSpec) {
			if cr.Spec.Select.HPA != nil {
				newSpec.Replicas = nil
			}
		},
	}
	return reconcile.Deployment(ctx, rclient, newDep, prevDep, &owner, &o)
}

// storageNodeURLs returns urls of all storage nodes, which must be queried by select nodes
func storageNodeURLs(cr *vmv1.VMEstimator) []string {
	storage := cr.Spec.Storage
	if storage == nil {
		return nil
	}
	scheme := vmv1beta1.HTTPProtoFromFlags(storage.ExtraArgs)
	pathPrefix := strings.TrimSuffix(vmv1beta1.BuildPathWithPrefixFlag(storage.ExtraArgs, "/"), "/")
	if pathPrefix != "" && !strings.HasPrefix(pathPrefix, "/") {
		pathPrefix = "/" + pathPrefix
	}
	// statefulset has a single replica by default
	replicas := ptr.Deref(storage.ReplicaCount, 1)
	urls := make([]string, 0, replicas)
	for i := range replicas {
		addr := vmv1beta1.PodDNSAddress(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), i, cr.Namespace, storage.Port, cr.Spec.ClusterDomainName)
		urls = append(urls, fmt.Sprintf("%s://%s%s", scheme, addr, pathPrefix))
	}
	return urls
}

func buildSelectDeployment(cr *vmv1.VMEstimator) (*appsv1.Deployment, error) {

	s := cr.Spec.Select
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if s.UpdateStrategy != nil {
		strategyType = *s.UpdateStrategy
	}
	podSpec, err := makePodSpecForSelect(cr)
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentSelect),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             s.ReplicaCount,
			RevisionHistoryLimit: s.RevisionHistoryLimitCount,
			MinReadySeconds:      s.MinReadySeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: s.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(dep, &s.CommonAppsParams)
	return dep, nil
}

func makePodSpecForSelect(cr *vmv1.VMEstimator) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	selectSpec := cr.Spec.Select
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", selectSpec.Port),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if selectSpec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", selectSpec.LogLevel))
	}
	if selectSpec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", selectSpec.LogFormat))
	}
	if nodes := storageNodeURLs(cr); len(nodes) > 0 {
		args = append(args, fmt.Sprintf("-storageNode=%s", strings.Join(nodes, ",")))
	}
	if len(selectSpec.ExtraEnvs) > 0 || len(selectSpec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, selectSpec.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(selectSpec.Port).IntVal,
		},
	}

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, selectSpec.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, selectSpec.VolumeMounts...)

	for _, s := range selectSpec.Secrets {
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

	for _, c := range selectSpec.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, selectSpec.ExtraArgs, "-")
	build.SortArgs(args)

	selectContainer := corev1.Container{
		Name:                     "select",
		Image:                    selectSpec.Image.Reference(),
		ImagePullPolicy:          selectSpec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                selectSpec.Resources,
		Env:                      envs,
		EnvFrom:                  selectSpec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.Probe(&selectContainer, selectSpec, &selectSpec.CommonAppsParams)
	build.Lifecycle(&selectContainer, &selectSpec.CommonAppsParams)
	operatorContainers := []corev1.Container{selectContainer}

	build.AddStrictSecuritySettingsToContainers(operatorContainers, &selectSpec.CommonAppsParams)
	containers, err := k8stools.MergePatchContainers(operatorContainers, selectSpec.Containers)
	if err != nil {
		return nil, err
	}

	for i := range selectSpec.TopologySpreadConstraints {
		if selectSpec.TopologySpreadConstraints[i].LabelSelector == nil {
			selectSpec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			}
		}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentSelect),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentSelect),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     selectSpec.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}, nil
}
