package vmdistributedcluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"path"
	"sort"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// VMAuthConfig is the top-level structure for vmauth configuration.
type VMAuthConfig struct {
	UnauthorizedUser *VMAuthUser `yaml:"unauthorized_user,omitempty"`
}

// VMAuthUser defines the user settings for vmauth.
type VMAuthUser struct {
	URLMap []URLMap `yaml:"url_map"`
}

// URLMap defines a single URL mapping rule.
type URLMap struct {
	SrcPaths           []string `yaml:"src_paths"`
	URLPrefix          []string `yaml:"url_prefix"`
	DiscoverBackendIPs bool     `yaml:"discover_backend_ips"`
}

const (
	configMountName = "config-out"
	configMountPath = "/opt/vmauth-config"
	configFileName  = "config.yaml"
	configGZName    = "config.yaml.gz"
)

// buildVMAuthVMSelectRefs builds the URLMap entries for each vmselect in the vmclusters.
func buildVMAuthVMSelectURLMaps(vmClusters []*vmv1beta1.VMCluster) URLMap {
	prefixes := make([]string, 0, len(vmClusters))
	for _, vmCluster := range vmClusters {
		targetHostSuffix := fmt.Sprintf("%s.svc", vmCluster.Namespace)
		if vmCluster.Spec.ClusterDomainName != "" {
			targetHostSuffix += fmt.Sprintf(".%s", vmCluster.Spec.ClusterDomainName)
		}
		selectPort := "8481"
		if vmCluster.Spec.VMSelect != nil && vmCluster.Spec.VMSelect.Port != "" {
			selectPort = vmCluster.Spec.VMSelect.Port
		}
		prefixes = append(prefixes, fmt.Sprintf("http://srv+%s.%s:%s", vmCluster.PrefixedName(vmv1beta1.ClusterComponentSelect), targetHostSuffix, selectPort))
	}
	return URLMap{
		SrcPaths:           []string{"/.*"},
		URLPrefix:          prefixes,
		DiscoverBackendIPs: true,
	}
}

// buildVMAuthLBSecret builds a secret containing the vmauth configuration.
func buildVMAuthLBSecret(cr *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) (*corev1.Secret, error) {
	config := VMAuthConfig{
		UnauthorizedUser: &VMAuthUser{
			URLMap: []URLMap{buildVMAuthVMSelectURLMaps(vmClusters)},
		},
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal vmauth config: %w", err)
	}

	var buf bytes.Buffer
	if err := gzipConfig(&buf, configData); err != nil {
		return nil, fmt.Errorf("cannot gzip config for vmauth: %w", err)
	}

	lbScrt := &corev1.Secret{
		ObjectMeta: buildLBConfigMeta(cr),
		Data:       map[string][]byte{configGZName: buf.Bytes()},
	}
	return lbScrt, nil
}

func gzipConfig(buf *bytes.Buffer, conf []byte) error {
	w := gzip.NewWriter(buf)
	defer w.Close()
	if _, err := w.Write(conf); err != nil {
		return err
	}
	return nil
}

func buildLBConfigMeta(cr *vmv1alpha1.VMDistributedCluster) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}
}

func buildVMAuthLBDeployment(cr *vmv1alpha1.VMDistributedCluster) (*appsv1.Deployment, error) {
	spec := cr.GetVMAuthSpec()
	volumes := []corev1.Volume{
		{
			Name: configMountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	volumes = append(volumes, spec.Volumes...)
	vmounts := []corev1.VolumeMount{
		{
			MountPath: configMountPath,
			Name:      configMountName,
		},
	}
	vmounts = append(vmounts, spec.VolumeMounts...)

	// Add license volume and mount if provided
	volumes, vmounts = build.LicenseVolumeTo(volumes, vmounts, spec.License, vmv1beta1.SecretsDir)

	args := []string{
		fmt.Sprintf("-auth.config=%s", path.Join(configMountPath, configFileName)),
		"-configCheckInterval=30s",
	}
	if spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", spec.LogLevel))

	}
	if spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", spec.LogFormat))
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(spec.ExtraEnvs) > 0 || len(spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = build.AddExtraArgsOverrideDefaults(args, spec.ExtraArgs, "-")
	// Add license args if provided
	args = build.LicenseArgsTo(args, spec.License, vmv1beta1.SecretsDir)
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
	vmauthLBCnt = build.Probe(vmauthLBCnt, spec)

	secretKeySelector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		},
		Key: configFileName,
	}
	configReloader := build.ConfigReloaderContainer(false, cr, vmounts, secretKeySelector)
	containers := []corev1.Container{
		vmauthLBCnt,
		configReloader,
	}
	var err error

	build.AddStrictSecuritySettingsToContainers(spec.SecurityContext, containers, ptr.Deref(spec.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err = k8stools.MergePatchContainers(containers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if spec.UpdateStrategy != nil {
		strategyType = *spec.UpdateStrategy
	}
	lbDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: spec.RollingUpdate,
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
	build.DeploymentAddCommonParams(lbDep, ptr.Deref(spec.UseStrictSecurity, cfg.EnableStrictSecurity), &spec.CommonApplicationDeploymentParams)
	return lbDep, nil
}

func createOrUpdateVMAuthLBService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	spec := cr.GetVMAuthSpec()

	builder := func(r *vmv1alpha1.VMDistributedCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, vmv1beta1.ClusterComponentBalancer)
		b.SetFinalLabels(labels.Merge(b.FinalLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: "vmauth",
		}))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, spec.Port, nil)
	// Set custom name
	svc.Name = cr.Spec.VMAuth.Name

	var prevSvc *corev1.Service
	if prevCR != nil {
		b = builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.VMAuth.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb service: %w", err)
	}
	svs := build.VMServiceScrapeForServiceWithSpec(svc, spec)
	svs.Spec.Selector.MatchLabels[vmv1beta1.VMAuthLBServiceProxyTargetLabel] = "vmauth"
	if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb vmservicescrape: %w", err)
	}
	return nil
}

func createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	spec := cr.GetVMAuthSpec()

	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer)
	pdb := build.PodDisruptionBudget(b, spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMAuth.Spec.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentBalancer)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VMAuth.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func updateVMAuthLBSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) error {
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildLBConfigMeta(prevCR))
	}

	secret, err := buildVMAuthLBSecret(cr, vmClusters)
	if err != nil {
		return fmt.Errorf("cannot build vmauth lb secret: %w", err)
	}
	if err := reconcile.Secret(ctx, rclient, secret, prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}

	if err := ensureVMAuthRoleExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot check vmauth role: %w", err)
	}
	if err := ensureVMAuthRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot check vmauth role binding: %w", err)
	}

	return nil
}

func ensureVMAuthRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildRole(prevCR)
	}
	return reconcile.Role(ctx, rclient, buildRole(cr), prevRole)
}

func ensureVMAuthRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildRoleBinding(prevCR)
	}
	return reconcile.RoleBinding(ctx, rclient, buildRoleBinding(cr), prevRB)
}

func buildRole(cr *vmv1alpha1.VMDistributedCluster) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.FinalAnnotations(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func buildRoleBinding(cr *vmv1alpha1.VMDistributedCluster) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.FinalAnnotations(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		RoleRef: rbacv1.RoleRef{
			Name:     cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Kind:     "Role",
			APIGroup: "rbac.authorization.k8s.io",
		},
		Subjects: []rbacv1.Subject{
			{
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.Namespace,
				Kind:      "ServiceAccount",
			},
		},
	}
}

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) error {
	spec := cr.GetVMAuthSpec()

	updateVMAuthLBSecret(ctx, rclient, cr, prevCR, vmClusters)

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
	}
	var prevSA *corev1.ServiceAccount
	if prevCR != nil {
		prevSA = serviceAccount.DeepCopy()
	}

	if err := reconcile.ServiceAccount(ctx, rclient, serviceAccount, prevSA); err != nil {
		return fmt.Errorf("failed create service account: %w", err)
	}

	lbDep, err := buildVMAuthLBDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil {
		prevLB, err = buildVMAuthLBDeployment(prevCR)
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
	if spec.PodDisruptionBudget != nil {
		if err := createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create or update PodDisruptionBudget for vmauth lb: %w", err)
		}
	}
	return nil
}
