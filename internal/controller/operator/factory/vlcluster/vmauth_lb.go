package vlcluster

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildLBConfigSecretMeta(prevCR))
	}
	if err := reconcile.Secret(ctx, rclient, buildVMauthLBSecret(cr), prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}
	lbDep, err := buildVMauthLBDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		prevLB, err = buildVMauthLBDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev deployment for vmauth loadbalancing: %w", err)
		}
	}
	if err := reconcile.Deployment(ctx, rclient, lbDep, prevLB, false); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb deployment: %w", err)
	}
	if err := createOrUpdateVMAuthLBService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		if err := createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create or update PodDisruptionBudget for vmauth lb: %w", err)
		}
	}
	return nil
}

func buildLBConfigSecretMeta(cr *vmv1.VLCluster) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
		Annotations:     cr.FinalAnnotations(),
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}
}

func buildVMauthLBSecret(cr *vmv1.VLCluster) *corev1.Secret {
	targetHostSuffix := fmt.Sprintf("%s.svc", cr.Namespace)
	if cr.Spec.ClusterDomainName != "" {
		targetHostSuffix += fmt.Sprintf(".%s", cr.Spec.ClusterDomainName)
	}
	insertPort := "9481"
	selectPort := "9471"
	insertProto := "http"
	selectProto := "http"
	if cr.Spec.VLSelect != nil {
		selectPort = cr.Spec.VLSelect.Port
		if v, ok := cr.Spec.VLSelect.ExtraArgs["tls"]; ok && v == "true" {
			selectProto = "https"
		}
	}
	if cr.Spec.VLInsert != nil {
		insertPort = cr.Spec.VLInsert.Port
		if v, ok := cr.Spec.VLInsert.ExtraArgs["tls"]; ok && v == "true" {
			selectProto = "https"
		}
	}
	insertURL := fmt.Sprintf("%s://%s.%s:%s",
		insertProto, cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert), targetHostSuffix, insertPort)
	selectURL := fmt.Sprintf("%s://%s.%s:%s",
		selectProto, cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect), targetHostSuffix, selectPort)

	lbScrt := &corev1.Secret{
		ObjectMeta: buildLBConfigSecretMeta(cr),
		// TODO: add backend auth
		StringData: map[string]string{"config.yaml": fmt.Sprintf(`
unauthorized_user:
  url_map:
  - src_paths:
    - "/insert/.*"
    - "/internal/insert"
    url_prefix: "%s"
    discover_backend_ips: true
  - src_paths:
    - ".*"
    url_prefix: "%s"
    discover_backend_ips: true
      `, insertURL,
			selectURL,
		)},
	}
	return lbScrt
}

func buildVMauthLBDeployment(cr *vmv1.VLCluster) (*appsv1.Deployment, error) {
	spec := cr.Spec.RequestsLoadBalancer.Spec
	const configMountName = "vmauth-lb-config"
	volumes := []corev1.Volume{
		{
			Name: configMountName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
				},
			},
		},
	}
	volumes = append(volumes, spec.Volumes...)
	vmounts := []corev1.VolumeMount{
		{
			MountPath: "/opt/vmauth-config/",
			Name:      configMountName,
		},
	}
	vmounts = append(vmounts, spec.VolumeMounts...)
	cfg := config.MustGetBaseConfig()

	args := []string{
		"-auth.config=/opt/vmauth-config/config.yaml",
		"-configCheckInterval=30s",
	}
	if spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", spec.LogLevel))

	}
	if spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", spec.LogFormat))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(spec.ExtraEnvs) > 0 || len(spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = build.AddExtraArgsOverrideDefaults(args, spec.ExtraArgs, "-")
	sort.Strings(args)
	vmauthLBCnt := corev1.Container{
		Name: "vmauth",
		Ports: []corev1.ContainerPort{
			{
				Protocol:      corev1.ProtocolTCP,
				Name:          "http",
				ContainerPort: intstr.Parse(spec.Port).IntVal,
			},
		},
		Args:            args,
		Env:             spec.ExtraEnvs,
		EnvFrom:         spec.ExtraEnvsFrom,
		Resources:       spec.Resources,
		Image:           fmt.Sprintf("%s:%s", spec.Image.Repository, spec.Image.Tag),
		ImagePullPolicy: spec.Image.PullPolicy,
		VolumeMounts:    vmounts,
	}
	vmauthLBCnt = build.Probe(vmauthLBCnt, &spec)
	containers := []corev1.Container{
		vmauthLBCnt,
	}
	var err error

	build.AddStrictSecuritySettingsToContainers(spec.SecurityContext, containers, ptr.Deref(spec.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err = k8stools.MergePatchContainers(containers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.RequestsLoadBalancer.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.RequestsLoadBalancer.Spec.UpdateStrategy
	}
	lbDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RequestsLoadBalancer.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(vmv1beta1.ClusterComponentBalancer),
					Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentBalancer),
				},
				Spec: corev1.PodSpec{
					Volumes:            volumes,
					InitContainers:     spec.InitContainers,
					Containers:         containers,
					ServiceAccountName: cr.GetServiceAccountName(),
				},
			},
		},
	}
	build.DeploymentAddCommonParams(lbDep, ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity, cfg.EnableStrictSecurity), &spec.CommonApplicationDeploymentParams)

	return lbDep, nil

}

func createOrUpdateVMAuthLBService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	builder := func(r *vmv1.VLCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, vmv1beta1.ClusterComponentBalancer)
		b.SetFinalLabels(labels.Merge(b.FinalLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: "vmauth",
		}))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, cr.Spec.RequestsLoadBalancer.Spec.Port, nil)

	var prevSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		b := builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.RequestsLoadBalancer.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb service: %w", err)
	}
	svs := build.VMServiceScrapeForServiceWithSpec(svc, &cr.Spec.RequestsLoadBalancer.Spec)
	svs.Spec.Selector.MatchLabels[vmv1beta1.VMAuthLBServiceProxyTargetLabel] = "vmauth"
	if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb vmservicescrape: %w", err)
	}
	return nil
}

func createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer)
	pdb := build.PodDisruptionBudget(b, cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentBalancer)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

// createOrUpdateLBProxyService builds vlinsert and vlselect external services to expose vlcluster components for access by vmauth
func createOrUpdateLBProxyService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster, kind vmv1beta1.ClusterComponent, port, prevPort string) error {
	builder := func(r *vmv1.VLCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, kind)
		b.SetFinalLabels(labels.Merge(b.FinalLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: string(kind),
		}))
		b.SetSelectorLabels(cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, cr.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
		svc.Spec.Ports[0].Port = intstr.Parse(port).IntVal
	})
	var prevSvc *corev1.Service
	if prevCR != nil {
		b := builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
			svc.Spec.Ports[0].Port = intstr.Parse(prevPort).IntVal
		})
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile lb service: %w", err)
	}
	return nil
}
