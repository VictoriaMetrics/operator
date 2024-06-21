package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type k8sWatcher struct {
	c          client.WithWatch
	inf        cache.SharedIndexInformer
	events     chan syncEvent
	namespace  string
	secretName string
	wg         sync.WaitGroup
}

type syncEvent struct {
	op  string
	obj *corev1.Secret
}

func newKubernetesWatcher(ctx context.Context, secretName, namespace string) (*k8sWatcher, error) {
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
	listOpts := &client.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", secretName),
	}
	inf := cache.NewSharedIndexInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			var s corev1.SecretList
			if err := c.List(ctx, &s, listOpts); err != nil {
				k8sAPIWatchErrorsTotal.Inc()
				return nil, fmt.Errorf("cannot get secret from k8s api: %w", err)
			}

			return &s, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			wi, err := c.Watch(ctx, &corev1.SecretList{}, listOpts)
			if err != nil {
				k8sAPIWatchErrorsTotal.Inc()
			}
			return wi, err
		},
	}, &corev1.Secret{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	syncChan := make(chan syncEvent, 10)
	if _, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			s := obj.(*corev1.Secret)
			syncChan <- syncEvent{op: "create", obj: s}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			s := newObj.(*corev1.Secret)
			syncChan <- syncEvent{op: "update", obj: s}
		},
		DeleteFunc: func(obj interface{}) {
			s := obj.(*corev1.Secret)
			syncChan <- syncEvent{op: "delete", obj: s}
		},
	}); err != nil {
		return nil, fmt.Errorf("cannot build eventHandler: %w", err)
	}

	return &k8sWatcher{inf: inf, c: c, events: syncChan, namespace: namespace, secretName: secretName}, nil
}

var errNotModified = fmt.Errorf("file content not modified")

func (k *k8sWatcher) startWatch(ctx context.Context, updates chan struct{}) error {
	var prevContent []byte
	updateSecret := func(secret *corev1.Secret) error {
		newData, ok := secret.Data[*configSecretKey]
		if !ok {
			return fmt.Errorf("key=%q with content not found at secret=%q", *configSecretKey, secret.Name)
		}
		if bytes.Equal(prevContent, newData) {
			logger.Infof("secret config update not needed,file content the same")
			return errNotModified
		}
		logger.Infof("updating local file content for secret: %s", secret.Name)
		if err := writeNewContent(newData); err != nil {
			return fmt.Errorf("cannot write file content to disk: %w", err)
		}
		prevContent = newData
		time.Sleep(time.Second)
		select {
		case updates <- struct{}{}:
		default:

		}
		return nil
	}
	go k.inf.Run(ctx.Done())

	var lastSecret corev1.Secret
	if err := k.c.Get(ctx, types.NamespacedName{Namespace: k.namespace, Name: k.secretName}, &lastSecret); err != nil {
		logger.Fatalf("cannot get secret during init secretName: %s, namespace: %s, err: %s", k.secretName, k.namespace, err)
	}
	if err := updateSecret(&lastSecret); err != nil {
		if *onlyInitConfig {
			return err
		}
		logger.Errorf("cannot update secret: %s", err)
	}
	k.wg.Add(1)

	go func() {
		defer k.wg.Done()
		var t time.Ticker
		if *resyncInternal > 0 {
			t = *time.NewTicker(*resyncInternal)
			defer t.Stop()
		}
		for {
			select {
			case <-t.C:
				if err := updateSecret(&lastSecret); err != nil {
					if errors.Is(err, errNotModified) {
						continue
					}
					contentUpdateErrosTotal.Inc()
					logger.Errorf("cannot force sync secret content: %s", err)
				}
			case item := <-k.events:
				s := item.obj
				lastSecret = *s
				logger.Infof("get k8s sync event type: %s, for secret: %s", item.op, item.obj.Name)

				if err := updateSecret(s); err != nil {
					if errors.Is(err, errNotModified) {
						continue
					}
					contentUpdateErrosTotal.Inc()
					logger.Errorf("cannot sync secret content: %s", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (k *k8sWatcher) close() {
	k.wg.Wait()
}
