package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/envflag"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/procutil"
	"github.com/fsnotify/fsnotify"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	configFileName   = flag.String("config-file", "", "config file watched by reloader")
	configFileDst    = flag.String("config-envsubst-file", "", "target file, where conent of configFile or configSecret would be written")
	configSecretName = flag.String("config-secret-name", "", "name of kubernetes secret in form of namespace/name")
	configSecretKey  = flag.String("config-secret-key", "config.yaml.gz", "key of config-secret-name for retrieving configuration from")
	watchInterval    = flag.Duration("watch-interval", time.Minute*3, "no-op for prometheus config-reloader compatability")
	delayInterval    = flag.Duration("delay-interval", time.Second, "")
	watchedDir       = flag.String("watched-dir", "", "directory to watch non-recursively")
	rulesDir         = flag.String("rules-dir", "", "the same as watched-dir, legacy")
	reloadURL        = flag.String("reload-url", "http://127.0.0.1:8429/-/reload", "reload URL to trigger config reload")
	listenAddr       = flag.String("http.listenAddr", ":8435", "http server listen addr")
)

func main() {
	envflag.Parse()
	buildinfo.Init()
	logger.Init()
	ctx, cancel := context.WithCancel(context.Background())
	logger.Infof("starting config reloader")
	r := reloader{
		c: http.DefaultClient,
	}
	updatesChan := make(chan struct{}, 10)
	configWatcher, err := newConfigWatcher(ctx, updatesChan)
	if err != nil {
		logger.Fatalf("cannot create configWatcher: %s", err)
	}
	configWatcher.startWatch(ctx, updatesChan)
	watcher := cfgWatcher{
		updates:  updatesChan,
		reloader: r.reload,
	}
	watcher.start(ctx)
	go httpserver.Serve(*listenAddr, requestHandler)
	procutil.WaitForSigterm()
	logger.Infof("received stop signal, stopping config-reloader")
	cancel()
	watcher.close()
	configWatcher.close()
	logger.Infof("config-reloader stopped")
}

type cfgWatcher struct {
	updates  chan struct{}
	reloader func(ctx context.Context) error
	wg       sync.WaitGroup
}

type reloader struct {
	c *http.Client
}

func (r *reloader) reload(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", *reloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := r.c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code: %d for reload api request: %w", resp.StatusCode, err)
	}
	return nil
}

func (c *cfgWatcher) start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-c.updates:
				if err := c.reloader(ctx); err != nil {
					logger.Errorf("cannot trigger api reload: %s", err)
					continue
				}
				logger.Infof("reload config ok.")
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *cfgWatcher) close() {
	c.wg.Wait()
}

func newConfigWatcher(ctx context.Context, dst chan struct{}) (watcher, error) {
	var w watcher
	if *configFileName == "" && *configSecretName == "" {
		return nil, fmt.Errorf("provide at least one configFileName")
	}
	if *configFileName != "" {
		fw, err := newFileWatcher(*configFileName)
		if err != nil {
			return nil, fmt.Errorf("cannot create file watcher: %w", err)
		}
		w = fw
	}
	if *configSecretName != "" {
		secretNamespaced := *configSecretName
		if *configSecretKey == "" {
			return nil, fmt.Errorf("config-secret-key cannot me empty")
		}
		logger.Infof("starting kubernetes watcher with secret: %s", secretNamespaced)
		idx := strings.IndexByte(secretNamespaced, '/')
		if idx <= 0 {
			return nil, fmt.Errorf("bad configSecretName: %s, it must be in form namspace/secret-name", secretNamespaced)
		}
		namespace := secretNamespaced[:idx]
		secretName := secretNamespaced[idx+1:]
		logger.Infof("starting watch for secret: %s at namespace: %s", secretName, namespace)
		kw, err := newKubernetesWatcher(ctx, secretName, namespace)
		if err != nil {
			return nil, fmt.Errorf("cannot create kubernetes watcher: %w", err)
		}
		w = kw
	}

	return w, nil
}

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

var firstGzipBytes = []byte{0x1f, 0x8b, 0x08}

func writeNewContent(data []byte) error {
	// fast path.
	if *configFileDst == "" {
		return nil
	}
	if len(data) > 3 && bytes.Equal(data[0:3], firstGzipBytes) {
		// its gzipped data
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("cannot create gzip reader: %w", err)
		}
		data, err = io.ReadAll(gz)
		if err != nil {
			return fmt.Errorf("cannot ungzip data: %w", err)
		}
	}
	if err := os.WriteFile(*configFileDst, data, 0644); err != nil {
		return fmt.Errorf("cannot write file: %s to the disk: %w", *configFileDst, err)
	}
	return nil
}

type fileWatcher struct {
	wg sync.WaitGroup
	w  *fsnotify.Watcher
}

func newFileWatcher(file string) (*fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(file); err != nil {
		return nil, err
	}
	return &fileWatcher{
		w: w,
	}, nil
}

type watcher interface {
	startWatch(ctx context.Context, updates chan struct{})
	close()
}

func (fw *fileWatcher) startWatch(ctx context.Context, updates chan struct{}) {
	fw.wg.Add(1)
	logger.Infof("starting file watcher")
	var prevContent []byte
	update := func(fileName string) error {
		newData, err := readFileContent(fileName)
		if err != nil {
			logger.Errorf("cannot read file: %s content, err: %s", fileName, err)
			return fmt.Errorf("cannot read file: %s content, err: %w", fileName, err)
		}
		if bytes.Equal(prevContent, newData) {
			logger.Infof("files the same, nothing to do")
			return errNotModified
		}
		if err := writeNewContent(newData); err != nil {
			return fmt.Errorf("cannot write content to file: %s, err: %w", fileName, err)
		}

		prevContent = newData
		select {
		case updates <- struct{}{}:
		default:

		}
		return nil
	}
	if err := update(*configFileName); err != nil {
		logger.Errorf("cannot update file on init")
	}
	go func() {
		defer fw.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-fw.w.Events:
				if event.Op == fsnotify.Remove {

					if err := fw.w.Add(event.Name); err != nil {
						logger.Errorf("cannot add new file: %s to watcter, err:%s", event.Name, err)
					}
				}
				if err := update(event.Name); err != nil {
					logger.Errorf("cannot update file :%s", err)
					continue
				}
			}
		}
	}()
}

func (fw *fileWatcher) close() {
	fw.wg.Wait()
}

func readFileContent(src string) ([]byte, error) {
	return os.ReadFile(src)
}

func requestHandler(w http.ResponseWriter, r *http.Request) bool {

	return false
}
