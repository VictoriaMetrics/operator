package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/envflag"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/flagutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/procutil"
	"github.com/VictoriaMetrics/metrics"
	"github.com/pires/go-proxyproto"
)

var (
	onlyInitConfig = flag.Bool(
		"only-init-config", false, "enables will read config and write to config-envsubst-file once before exit")
	configFileName = flag.String(
		"config-file", "", "config file watched by reloader")
	configFileDst = flag.String(
		"config-envsubst-file", "", "target file, where conent of configFile or configSecret would be written")
	configSecretName = flag.String(
		"config-secret-name", "", "name of kubernetes secret in form of namespace/name")
	configSecretKey = flag.String(
		"config-secret-key", "config.yaml.gz", "key of config-secret-name for retrieving configuration from")
	_ = flag.Duration(
		"watch-interval", time.Minute*3, "no-op for prometheus config-reloader compatibility")
	delayInterval = flag.Duration(
		"delay-interval", 3*time.Second, "delays config reload time.")
	watchedDir = flagutil.NewArrayString(
		"watched-dir", "directory to watch non-recursively")
	rulesDir = flagutil.NewArrayString(
		"rules-dir", "the same as watched-dir, legacy")
	reloadURL = flag.String(
		"reload-url", "http://127.0.0.1:8429/-/reload", "reload URL to trigger config reload")
	listenAddr = flag.String(
		"http.listenAddr", ":8435", "http server listen addr")
	useProxyProtocol = flagutil.NewArrayBool(
		"reload-use-proxy-protocol", "enables proxy-protocol for reload connections.")
	resyncInternal = flag.Duration(
		"resync-interval", 0, "interval for force resync of the last configuration")
	webhookMethod = flag.String(
		"webhook-method", "GET", "the HTTP method url to use to send the webhook")
)

var (
	configLastOkReloadTime  = metrics.NewCounter(`configreloader_last_reload_success_timestamp_seconds`)
	configLastReloadSuccess = metrics.NewCounter(`configreloader_last_reload_successful`)
	configReloadErrorsTotal = metrics.NewCounter(`configreloader_last_reload_errors_total`)
	configReloadsTotal      = metrics.NewCounter(`configreloader_config_last_reload_total`)
	k8sAPIWatchErrorsTotal  = metrics.NewCounter(`configreloader_k8s_watch_errors_total`)
	contentUpdateErrosTotal = metrics.NewCounter(`configreloader_secret_content_update_errors_total`)
)

func main() {
	envflag.Parse()
	buildinfo.Init()
	logger.Init()
	ctx, cancel := context.WithCancel(context.Background())
	logger.Infof("starting config reloader")
	r := reloader{
		c: buildHTTPClient(),
	}
	updatesChan := make(chan struct{}, 10)
	configWatcher, err := newConfigWatcher(ctx)
	if err != nil {
		logger.Fatalf("cannot create configWatcher: %s", err)
	}

	err = configWatcher.startWatch(ctx, updatesChan)
	if *onlyInitConfig {
		if err != nil {
			logger.Fatalf("failed to init config: %v", err)
		}
		logger.Infof("config initiation succeed, exit now")
		cancel()
		configWatcher.close()
		return
	}
	watcher := cfgWatcher{
		updates:  updatesChan,
		reloader: r.reload,
	}
	watcher.start(ctx)
	var dws []string
	if len(*watchedDir) > 0 {
		dws = *watchedDir
	} else if len(*rulesDir) > 0 {
		dws = *rulesDir
	}

	dw, err := newDirWatchers(dws)
	if err != nil {
		logger.Fatalf("cannot start dir watcher: %s", err)
	}
	dw.startWatch(ctx, updatesChan)
	go httpserver.Serve([]string{*listenAddr}, useProxyProtocol, requestHandler)
	procutil.WaitForSigterm()
	logger.Infof("received stop signal, stopping config-reloader")
	cancel()
	watcher.close()
	configWatcher.close()
	dw.close()
	logger.Infof("config-reloader stopped")
}

var connTimeout = 10 * time.Second

func buildHTTPClient() *http.Client {
	t := (http.DefaultTransport.(*http.Transport)).Clone()
	d := &net.Dialer{
		Timeout: connTimeout,
	}
	t.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := d.Dial(network, addr)
		if err != nil {
			return nil, err
		}
		if !useProxyProtocol.GetOptionalArg(0) {
			return conn, nil
		}
		header := &proxyproto.Header{
			Version:           2,
			Command:           proxyproto.PROXY,
			TransportProtocol: proxyproto.TCPv4,
			SourceAddr:        conn.LocalAddr(),
			DestinationAddr:   conn.RemoteAddr(),
		}
		_, err = header.WriteTo(conn)
		if err != nil {
			return nil, fmt.Errorf("cannot write proxy protocol header: %w", err)
		}
		return conn, nil
	}

	return &http.Client{
		Timeout:   connTimeout,
		Transport: t,
	}
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
	configReloadsTotal.Inc()
	req, err := http.NewRequestWithContext(ctx, *webhookMethod, *reloadURL, nil)
	if err != nil {
		return fmt.Errorf("cannot build request for reload api: %w", err)
	}
	resp, err := r.c.Do(req)
	if err != nil {
		return fmt.Errorf("cannot execute request for reload api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code: %d for reload api request", resp.StatusCode)
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
				go func() {
					if *delayInterval > 0 {
						t := time.NewTimer(*delayInterval)
						defer t.Stop()
						select {
						case <-t.C:
						case <-ctx.Done():
							return
						}
					}
					if err := c.reloader(ctx); err != nil {
						logger.Errorf("cannot trigger api reload: %s", err.Error())
						configLastReloadSuccess.Set(0)
						configReloadErrorsTotal.Inc()
						return
					}
					configLastReloadSuccess.Set(1)
					configLastOkReloadTime.Set(uint64(time.Now().UnixMilli()))
					logger.Infof("reload config ok.")
				}()

			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *cfgWatcher) close() {
	c.wg.Wait()
}

type watcher interface {
	startWatch(ctx context.Context, updates chan struct{}) error
	close()
}

// emptyWatcher - no-op watchers for case when direct watch not required
type emptyWatcher struct{}

func (ew *emptyWatcher) startWatch(_ context.Context, _ chan struct{}) error {
	return nil
}

func (ew *emptyWatcher) close() {}

func newConfigWatcher(ctx context.Context) (watcher, error) {
	var w watcher
	if *configFileName == "" && *configSecretName == "" {
		logger.Infof("direct config watch not needed, both configFileName and configSecretName is empty")
		return &emptyWatcher{}, nil
	}
	if *configFileName != "" && *configSecretName != "" {
		logger.Infof(
			"both config have been provided, will use configSecret %s instead of configFile %s",
			*configSecretName, *configFileName)
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
			return nil, fmt.Errorf("config-secret-key cannot be empty")
		}
		logger.Infof("starting kubernetes watcher with secret: %s", secretNamespaced)
		idx := strings.IndexByte(secretNamespaced, '/')
		if idx <= 0 {
			return nil, fmt.Errorf("bad configSecretName: %s, it must be in form namespace/secret-name", secretNamespaced)
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
		defer gz.Close()
		data, err = io.ReadAll(gz)
		if err != nil {
			return fmt.Errorf("cannot ungzip data: %w", err)
		}
	}
	tmpDst := *configFileDst + ".tmp"
	if err := os.WriteFile(tmpDst, data, 0644); err != nil {
		return fmt.Errorf("cannot write file: %s to the disk: %w", *configFileDst, err)
	}
	if err := os.Rename(tmpDst, *configFileDst); err != nil {
		return fmt.Errorf("cannot rename tmp file: %w", err)
	}
	return nil
}

func requestHandler(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/metrics":
		w.WriteHeader(http.StatusOK)
		metrics.WritePrometheus(w, true)
	case "/health":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`OK`))
	}
	return false
}
