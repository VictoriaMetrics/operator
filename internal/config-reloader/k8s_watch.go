package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type k8sWatcher struct {
	c          client.WithWatch
	namespace  string
	secretName string
	w          watch.Interface
	wg         sync.WaitGroup
}

func newKubernetesWatcher(ctx context.Context, secretName, namespace string) (*k8sWatcher, error) {
	var objs v1.SecretList
	lr := clientcmd.NewDefaultClientConfigLoadingRules()

	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(lr, &clientcmd.ConfigOverrides{})
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot read client cfg from kubeconfig: %w", err)
	}
	c, err := client.NewWithWatch(restCfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("cannot start watch for secret: %w", err)
	}
	w, err := c.Watch(ctx, &objs, &client.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", secretName),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot start watch for secretName: %s at namespace: %s, err: %w", secretName, namespace, err)
	}
	return &k8sWatcher{w: w, c: c, namespace: namespace, secretName: secretName}, nil
}

var errNotModified = fmt.Errorf("file content not modified")

func (k *k8sWatcher) startWatch(ctx context.Context, updates chan struct{}) {

	var prevContent []byte
	updateSecret := func(secret *v1.Secret) error {
		newData, ok := secret.Data[*configSecretKey]
		if !ok {
			// bad case no such key.
			logger.Warnf("key not found")
		}
		if bytes.Equal(prevContent, newData) {
			logger.Infof("file content the same")
			return errNotModified
		}
		if err := writeNewContent(newData); err != nil {
			logger.Errorf("cannot write file content to disk: %s", err)
			return err
		}
		prevContent = newData
		time.Sleep(time.Second)
		select {
		case updates <- struct{}{}:
		default:

		}
		return nil
	}

	var s v1.Secret
	if err := k.c.Get(ctx, types.NamespacedName{Namespace: k.namespace, Name: k.secretName}, &s); err != nil {
		logger.Fatalf("cannot get secret during init secretName: %s, namespace: %s, err: %s", k.secretName, k.namespace, err)
	}
	if err := updateSecret(&s); err != nil {
		logger.Errorf("cannot update secret: %s", err)
	}
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		defer k.w.Stop()

		for {
			select {
			case item := <-k.w.ResultChan():
				switch item.Type {
				case watch.Added, watch.Modified, watch.Deleted:
				default:
					continue
				}
				s := item.Object.(*v1.Secret)
				if err := updateSecret(s); err != nil {
					if errors.Is(err, errNotModified) {
						continue
					}
					logger.Errorf("cannot sync secret content: %s", err)
				}
			case <-ctx.Done():
				return
			}
		}

	}()
}

func (k *k8sWatcher) close() {
	k.wg.Wait()
}
