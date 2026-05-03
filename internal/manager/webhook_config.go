package manager

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// webhookObjects is the canonical list of API objects for which validating webhooks are
// registered. It must stay in sync with the setup functions called in addWebhooks.
var webhookObjects = []client.Object{
	&vmv1beta1.VMAgent{},
	&vmv1beta1.VMAlert{},
	&vmv1beta1.VMSingle{},
	&vmv1beta1.VMCluster{},
	&vmv1beta1.VMAuth{},
	&vmv1beta1.VMUser{},
	&vmv1beta1.VMAlertmanager{},
	&vmv1beta1.VMAlertmanagerConfig{},
	&vmv1beta1.VMRule{},
	&vmv1beta1.VMServiceScrape{},
	&vmv1beta1.VMPodScrape{},
	&vmv1beta1.VMNodeScrape{},
	&vmv1beta1.VMScrapeConfig{},
	&vmv1beta1.VMStaticScrape{},
	&vmv1beta1.VMProbe{},
	&vmv1beta1.VLogs{},
	&vmv1.VMAnomaly{},
	&vmv1.VMAnomalyConfig{},
	&vmv1.VLSingle{},
	&vmv1.VLCluster{},
	&vmv1.VLAgent{},
	&vmv1.VTSingle{},
	&vmv1.VTCluster{},
	&vmv1alpha1.VMDistributed{},
}

type webhookDef struct {
	name     string
	path     string
	version  string
	resource string
}

type webhookConfig struct {
	client                kubernetes.Interface
	scheme                *runtime.Scheme
	restMapper            apimeta.RESTMapper
	configName            string
	serviceName           string
	serviceNamespace      string
	caBundle              []byte
	failurePolicy         admissionregistrationv1.FailurePolicyType
	certManagerInjectFrom string
}

// registerWebhookConfig builds and registers the webhookConfig runnable
// using the global flag values set in manager.go.
func registerWebhookConfig(mgr ctrl.Manager, client kubernetes.Interface) error {
	ns := *webhookServiceNamespace
	if ns == "" {
		raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return fmt.Errorf("webhook.serviceNamespace not set and cannot read in-cluster namespace: %w", err)
		}
		ns = strings.TrimSpace(string(raw))
	}

	var caBundle []byte
	if *webhookCertManagerInjectFrom == "" {
		caPath := path.Join(*webhookCertDir, *webhookCACertName)
		var err error
		caBundle, err = os.ReadFile(caPath)
		if err != nil {
			return fmt.Errorf("cannot read webhook CA cert from %q: %w", caPath, err)
		}
	}

	failurePolicy := admissionregistrationv1.Fail
	if *webhookFailurePolicy == "Ignore" {
		failurePolicy = admissionregistrationv1.Ignore
	}

	r := &webhookConfig{
		client:                client,
		scheme:                mgr.GetScheme(),
		restMapper:            mgr.GetRESTMapper(),
		configName:            *webhookConfigName,
		serviceName:           *webhookServiceName,
		serviceNamespace:      ns,
		caBundle:              caBundle,
		failurePolicy:         failurePolicy,
		certManagerInjectFrom: *webhookCertManagerInjectFrom,
	}
	if err := mgr.Add(r); err != nil {
		return fmt.Errorf("cannot add webhook config runnable: %w", err)
	}
	return nil
}

func (r *webhookConfig) Start(ctx context.Context) error {
	if err := r.sync(ctx); err != nil {
		return fmt.Errorf("cannot register ValidatingWebhookConfiguration %q: %w", r.configName, err)
	}
	setupLog.Info("registered ValidatingWebhookConfiguration", "name", r.configName)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := r.delete(shutdownCtx); err != nil {
		setupLog.Error(err, "cannot deregister ValidatingWebhookConfiguration on shutdown", "name", r.configName)
	} else {
		setupLog.Info("deregistered ValidatingWebhookConfiguration", "name", r.configName)
	}
	return nil
}

func (r *webhookConfig) sync(ctx context.Context) error {
	desired, err := r.build()
	if err != nil {
		return err
	}
	_, err = r.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, r.configName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = r.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing, err := r.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, r.configName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.Webhooks = desired.Webhooks
	existing.Annotations = desired.Annotations
	_, err = r.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (r *webhookConfig) delete(ctx context.Context) error {
	err := r.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, r.configName, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *webhookConfig) build() (*admissionregistrationv1.ValidatingWebhookConfiguration, error) {
	defs, err := r.buildDefs()
	if err != nil {
		return nil, err
	}

	failPolicy := r.failurePolicy
	sideEffects := admissionregistrationv1.SideEffectClassNone

	webhooks := make([]admissionregistrationv1.ValidatingWebhook, 0, len(defs))
	for _, def := range defs {
		webhookPath := def.path
		wh := admissionregistrationv1.ValidatingWebhook{
			Name:                    def.name,
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             &sideEffects,
			FailurePolicy:           &failPolicy,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Name:      r.serviceName,
					Namespace: r.serviceNamespace,
					Path:      &webhookPath,
				},
				CABundle: r.caBundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{vmOperatorAPIGroup},
						APIVersions: []string{def.version},
						Resources:   []string{def.resource},
					},
				},
			},
		}
		webhooks = append(webhooks, wh)
	}

	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: r.configName},
		Webhooks:   webhooks,
	}
	if r.certManagerInjectFrom != "" {
		vwc.Annotations = map[string]string{
			"certmanager.k8s.io/inject-ca-from": r.certManagerInjectFrom,
			"cert-manager.io/inject-ca-from":    r.certManagerInjectFrom,
		}
	}
	return vwc, nil
}

const vmOperatorAPIGroup = "operator.victoriametrics.com"

// buildDefs derives webhook definitions from webhookObjects at runtime using
// the scheme (for GVK) and REST mapper (for plural resource name).
func (r *webhookConfig) buildDefs() ([]webhookDef, error) {
	defs := make([]webhookDef, 0, len(webhookObjects))
	for _, obj := range webhookObjects {
		gvks, _, err := r.scheme.ObjectKinds(obj)
		if err != nil {
			return nil, fmt.Errorf("cannot determine GVK for %T: %w", obj, err)
		}
		if len(gvks) == 0 {
			return nil, fmt.Errorf("no GVK registered for %T", obj)
		}
		gvk := gvks[0]

		mapping, err := r.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("cannot get REST mapping for %s: %w", gvk, err)
		}

		// Controller-runtime derives the webhook path with this same formula.
		webhookPath := "/validate-" + strings.ReplaceAll(gvk.Group, ".", "-") + "-" + gvk.Version + "-" + strings.ToLower(gvk.Kind)
		webhookName := fmt.Sprintf("v%s-%s.kb.io", strings.ToLower(gvk.Kind), gvk.Version)

		defs = append(defs, webhookDef{
			name:     webhookName,
			path:     webhookPath,
			version:  gvk.Version,
			resource: mapping.Resource.Resource,
		})
	}
	return defs, nil
}
