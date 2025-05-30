package vmalert

import (
	"context"
	"fmt"
	"sort"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createOrUpdateService creates service for vmalert
func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}

	newService := build.Service(cr, cr.Spec.Port, nil)

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(newService, s)
		if additionalSvc.Name == newService.Name {
			return fmt.Errorf("vmalert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, cr.PrefixedName())
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmalert: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmalert: %w", err)
	}
	return newService, nil
}

func createOrUpdateSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert, ssCache map[string]*authSecret) error {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Annotations:     cr.AnnotationsFiltered(),
			Labels:          cr.AllLabels(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
		},
		Data: map[string][]byte{},
	}
	addSecretKeys := func(sourcePrefix string, ha vmv1beta1.HTTPAuth) {
		ba := ssCache[sourcePrefix]
		if ba == nil {
			return
		}
		if ha.BasicAuth != nil && ba.BasicAuthCredentials != nil {
			if len(ba.Password) > 0 {
				s.Data[buildRemoteSecretKey(sourcePrefix, basicAuthPasswordKey)] = []byte(ba.Password)
			}
		}
		if ha.BearerAuth != nil && len(ba.bearerValue) > 0 {
			s.Data[buildRemoteSecretKey(sourcePrefix, bearerTokenKey)] = []byte(ba.bearerValue)
		}
		if ha.OAuth2 != nil && ba.OAuthCreds != nil {
			if len(ba.ClientSecret) > 0 {
				s.Data[buildRemoteSecretKey(sourcePrefix, oauth2SecretKey)] = []byte(ba.ClientSecret)
			}
		}
	}
	if cr.Spec.RemoteRead != nil {
		addSecretKeys(remoteReadKey, cr.Spec.RemoteRead.HTTPAuth)
	}
	if cr.Spec.RemoteWrite != nil {
		addSecretKeys(remoteWriteKey, cr.Spec.RemoteWrite.HTTPAuth)
	}
	addSecretKeys(datasourceKey, cr.Spec.Datasource.HTTPAuth)
	for idx, nf := range cr.Spec.Notifiers {
		addSecretKeys(buildNotifierKey(idx), nf.HTTPAuth)
	}
	var prevSecretMeta *metav1.ObjectMeta
	if cr.ParsedLastAppliedSpec != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:        prevCR.PrefixedName(),
			Annotations: prevCR.AnnotationsFiltered(),
			Labels:      prevCR.AllLabels(),
			Namespace:   prevCR.Namespace,
		}
	}

	return reconcile.Secret(ctx, rclient, s, prevSecretMeta)
}

// CreateOrUpdate creates vmalert deployment for given CRD
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAlert
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot delete objects from previous state: %w", err)
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}
	if err := discoverNotifierIfNeeded(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot discover additional notifiers: %w", err)
	}

	cmNames, err := CreateOrUpdateConfig(ctx, rclient, cr, nil)
	if err != nil {
		return err
	}

	remoteSecrets, err := loadVMAlertRemoteSecrets(ctx, rclient, cr)
	if err != nil {
		return err
	}
	// create secret for remoteSecrets
	if err := createOrUpdateSecret(ctx, rclient, cr, prevCR, remoteSecrets); err != nil {
		return err
	}

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create vmservicescrape: %w", err)
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmalert: %w", err)
		}
	}

	err = createOrUpdateTLSAssets(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeploy(prevCR, cmNames, remoteSecrets)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeploy(cr, cmNames, remoteSecrets)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmalert: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

// newDeploy returns a busybox pod with the same name/namespace as the cr
func newDeploy(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) (*appsv1.Deployment, error) {

	podSpec, err := newPodSpec(cr, ruleConfigMapNames, remoteSecrets)
	if err != nil {
		return nil, fmt.Errorf("cannot generate new spec for vmalert: %w", err)
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}

	app := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(),
					Annotations: cr.PodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}

	build.DeploymentAddCommonParams(app, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
	return app, nil
}

type authSecret struct {
	bearerValue string
	*k8stools.BasicAuthCredentials
	*k8stools.OAuthCreds
}

func loadVMAlertRemoteSecrets(
	ctx context.Context,
	rclient client.Client,
	cr *vmv1beta1.VMAlert,
) (map[string]*authSecret, error) {
	datasource := cr.Spec.Datasource
	remoteWrite := cr.Spec.RemoteWrite
	remoteRead := cr.Spec.RemoteRead
	ns := cr.Namespace

	nsSecretCache := make(map[string]*corev1.Secret)
	nsCMCache := make(map[string]*corev1.ConfigMap)
	authSecretsBySource := make(map[string]*authSecret)
	loadHTTPAuthSecrets := func(ctx context.Context, rclient client.Client, ns string, httpAuth vmv1beta1.HTTPAuth) (*authSecret, error) {
		var as authSecret
		if httpAuth.BasicAuth != nil {
			credentials, err := k8stools.LoadBasicAuthSecret(ctx, rclient, cr.Namespace, httpAuth.BasicAuth, nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("could not load basicAuth config. %w", err)
			}
			as.BasicAuthCredentials = &credentials
		}
		if httpAuth.BearerAuth != nil && httpAuth.TokenSecret != nil {
			token, err := k8stools.GetCredFromSecret(ctx, rclient, cr.Namespace, httpAuth.TokenSecret, buildCacheKey(ns, httpAuth.TokenSecret.Name), nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot load bearer auth token: %w", err)
			}
			as.bearerValue = token
		}
		if httpAuth.OAuth2 != nil {
			oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, httpAuth.OAuth2, cr.Namespace, nsSecretCache, nsCMCache)
			if err != nil {
				return nil, fmt.Errorf("cannot load oauth2 creds err: %w", err)
			}
			as.OAuthCreds = oauth2
		}
		return &as, nil
	}
	for i, notifier := range cr.Spec.Notifiers {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, notifier.HTTPAuth)
		if err != nil {
			return nil, err
		}
		authSecretsBySource[buildNotifierKey(i)] = as
	}
	// load basic auth for datasource configuration
	as, err := loadHTTPAuthSecrets(ctx, rclient, ns, datasource.HTTPAuth)
	if err != nil {
		return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
	}
	authSecretsBySource[datasourceKey] = as

	// load basic auth for remote write configuration
	if remoteWrite != nil {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, remoteWrite.HTTPAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		authSecretsBySource[remoteWriteKey] = as
	}
	// load basic auth for remote write configuration
	if remoteRead != nil {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, remoteRead.HTTPAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		authSecretsBySource[remoteReadKey] = as
	}
	return authSecretsBySource, nil
}

func createOrUpdateTLSAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert) error {
	assets, err := loadTLSAssets(ctx, rclient, cr)
	if err != nil {
		return fmt.Errorf("cannot load tls assets: %w", err)
	}

	tlsAssetsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.TLSAssetName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Data: map[string][]byte{},
	}

	for key, asset := range assets {
		tlsAssetsSecret.Data[key] = []byte(asset)
	}
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:        prevCR.TLSAssetName(),
			Labels:      prevCR.AllLabels(),
			Annotations: prevCR.AnnotationsFiltered(),
			Namespace:   prevCR.Namespace,
		}
	}
	return reconcile.Secret(ctx, rclient, tlsAssetsSecret, prevSecretMeta)
}

func loadTLSAssets(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)
	tlsConfigs := []*vmv1beta1.TLSConfig{}

	for _, notifier := range cr.Spec.Notifiers {
		if notifier.TLSConfig != nil {
			tlsConfigs = append(tlsConfigs, notifier.TLSConfig)
		}
	}
	if cr.Spec.RemoteRead != nil && cr.Spec.RemoteRead.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteRead.TLSConfig)
	}
	if cr.Spec.RemoteWrite != nil && cr.Spec.RemoteWrite.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteWrite.TLSConfig)
	}
	if cr.Spec.Datasource.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.Datasource.TLSConfig)
	}

	fetchAssetFor := func(assetPath string, src vmv1beta1.SecretOrConfigMap) error {
		var asset string
		var err error
		cacheKey := cr.Namespace + "/" + src.PrefixedName()
		switch {
		case src.Secret != nil:
			asset, err = k8stools.GetCredFromSecret(
				ctx,
				rclient,
				cr.Namespace,
				src.Secret,
				cacheKey,
				nsSecretCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset from secret %s and key %s in namespace %s",
					src.PrefixedName(), src.Key(), cr.Namespace,
				)
			}

		case src.ConfigMap != nil:
			asset, err = k8stools.GetCredFromConfigMap(
				ctx,
				rclient,
				cr.Namespace,
				*src.ConfigMap,
				cacheKey,
				nsConfigMapCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset for  configmap %v and key %v in namespace %v: %w",
					src.PrefixedName(), src.Key(), cr.Namespace, err,
				)
			}
		}
		if len(asset) > 0 {
			assets[assetPath] = asset
		}
		return nil
	}

	for _, rw := range tlsConfigs {
		if err := fetchAssetFor(rw.BuildAssetPath(cr.Namespace, rw.CA.PrefixedName(), rw.CA.Key()), rw.CA); err != nil {
			return nil, fmt.Errorf("cannot fetch tls asset for CA: %w", err)
		}
		if err := fetchAssetFor(rw.BuildAssetPath(cr.Namespace, rw.Cert.PrefixedName(), rw.Cert.Key()), rw.Cert); err != nil {
			return nil, fmt.Errorf("cannot fetch tls asset for Cert: %w", err)
		}

		if rw.KeySecret != nil {
			asset, err := k8stools.GetCredFromSecret(
				ctx,
				rclient,
				cr.Namespace,
				rw.KeySecret,
				cr.Namespace+"/"+rw.KeySecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint tls asset for vmservicescrape %s from secret %s and key %s in namespace %s",
					cr.Name, rw.CA.PrefixedName(), rw.CA.Key(), cr.Namespace,
				)
			}
			assets[rw.BuildAssetPath(cr.Namespace, rw.KeySecret.Name, rw.KeySecret.Key)] = asset
		}
	}

	return assets, nil
}

func buildCacheKey(ns, keyName string) string {
	return fmt.Sprintf("%s/%s", ns, keyName)
}

func discoverNotifierIfNeeded(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	var additionalNotifiers []vmv1beta1.VMAlertNotifierSpec

	if cr.Spec.Notifier != nil {
		cr.Spec.Notifiers = append(cr.Spec.Notifiers, *cr.Spec.Notifier)
	}
	// trim notifiers with non-empty notifier Selector
	var cnt int
	for i := range cr.Spec.Notifiers {
		n := cr.Spec.Notifiers[i]
		// fast path
		if n.Selector == nil {
			cr.Spec.Notifiers[cnt] = n
			cnt++
			continue
		}
		// discover alertmanagers
		var ams vmv1beta1.VMAlertmanagerList
		amListOpts, err := n.Selector.AsListOptions()
		if err != nil {
			return fmt.Errorf("cannot convert notifier selector as ListOptions: %w", err)
		}
		if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(objects *vmv1beta1.VMAlertmanagerList) {
			ams.Items = append(ams.Items, objects.Items...)
		}, amListOpts); err != nil {
			return fmt.Errorf("cannot list alertmanager with discovery selector: %w", err)
		}

		for _, item := range ams.Items {
			if !item.DeletionTimestamp.IsZero() || (n.Selector.Namespace != nil && !n.Selector.Namespace.IsMatch(&item)) {
				continue
			}
			dsc := item.AsNotifiers()
			additionalNotifiers = append(additionalNotifiers, dsc...)
		}
	}
	cr.Spec.Notifiers = cr.Spec.Notifiers[:cnt]

	if len(additionalNotifiers) > 0 {
		sort.Slice(additionalNotifiers, func(i, j int) bool {
			return additionalNotifiers[i].URL > additionalNotifiers[j].URL
		})
		logger.WithContext(ctx).Info(fmt.Sprintf("additional notifiers count=%d discovered with sd selectors", len(additionalNotifiers)))
	}
	cr.Spec.Notifiers = append(cr.Spec.Notifiers, additionalNotifiers...)
	return nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && cr.ParsedLastAppliedSpec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
