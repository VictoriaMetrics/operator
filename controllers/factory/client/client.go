package client

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// Client wraps controller-runtime and coreV1 client
// @F41gh7:  i suggest to migrate from controller-runtime to the corev1 client
// it uses less memory, see this issue https://github.com/VictoriaMetrics/operator/issues/285
type Client interface {
	GetCRClient() client.Client
	GetCoreClient() *v1.CoreV1Client
}

// WrappedClient implements interface
type WrappedClient struct {
	cr     client.Client
	coreV1 *v1.CoreV1Client
}

func NewClient(mgr controllerruntime.Manager, scheme *runtime.Scheme) (Client, error) {

	//cr, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	//if err != nil {
	//	return nil, fmt.Errorf("cannot init controller-runtime client: %w", err)
	//}
	coreV1, err := v1.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("cannot build corev1 client:  %w", err)
	}
	cr, err := cluster.New(mgr.GetConfig(), func(options *cluster.Options) {
		options.Scheme = scheme
	})
	if err != nil {
		return nil, err
	}
	go func() {
		if err := cr.Start(context.Background()); err != nil {
			panic(err)
		}
	}()
	fmt.Printf("wait for sync\n")
	b := cr.GetCache().WaitForCacheSync(context.Background())
	fmt.Printf("cache state: %v\n", b)
	return &WrappedClient{
		cr:     cr.GetClient(),
		coreV1: coreV1,
	}, nil
}

func (c *WrappedClient) GetCRClient() client.Client {
	return c.cr
}

func (c *WrappedClient) GetCoreClient() *v1.CoreV1Client {
	return c.coreV1
}

// NewTestClient returns client for testings
func NewTestClient(cr client.Client, coreV1 *v1.CoreV1Client) Client {
	return &WrappedClient{cr: cr, coreV1: coreV1}
}
