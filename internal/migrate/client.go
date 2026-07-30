// Package migrate implements the `helm-converter migrate` command: an imperative,
// cluster-touching tool that moves a standalone Helm-chart-deployed VictoriaMetrics/
// VictoriaLogs installation to an operator-managed CR.
package migrate

import (
	"fmt"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var scheme = newScheme()

func newScheme() *client.Options {
	s := clientgoscheme.Scheme
	utilruntime.Must(vmv1beta1.AddToScheme(s))
	utilruntime.Must(vmv1.AddToScheme(s))
	return &client.Options{Scheme: s}
}

// NewClient builds a controller-runtime client.Client from the given kubeconfig path (empty
// uses the standard kubeconfig loading rules).
func NewClient(kubeconfig string) (client.Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load kubeconfig: %w", err)
	}
	c, err := client.New(cfg, *scheme)
	if err != nil {
		return nil, fmt.Errorf("cannot build kubernetes client: %w", err)
	}
	return c, nil
}
