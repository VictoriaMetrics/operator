---
build:
  list: never
  publishResources: false
  render: never
sitemap:
  disable: true
---
<!-- The file is automatically updated by make docs command -->
```shellhelp
Usage of bin/config-reloader:
  -config-envsubst-file string
    	target file, where content of configFile or configSecret would be written
  -config-file string
    	config file watched by reloader
  -config-secret-key string
    	key of config-secret-name for retrieving configuration from (default "config.yaml.gz")
  -config-secret-name string
    	name of kubernetes secret in form of namespace/name
  -delay-interval duration
    	delays config reload time. (default 3s)
  -enableTCP6
    	Whether to enable IPv6 for listening and dialing. By default, only IPv4 TCP and UDP are used
  -envflag.enable
    	Whether to enable reading flags from environment variables in addition to the command line. Command line flag values have priority over values from environment vars. Flags are read only from the command line if this flag isn't set. See https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#environment-variables for more details
  -envflag.prefix string
    	Prefix for environment variables if -envflag.enable is set
  -filestream.disableFadvise
    	Whether to disable fadvise() syscall when reading large data files. The fadvise() syscall prevents from eviction of recently accessed data from OS page cache during background merges and backups. In some rare cases it is better to disable the syscall if it uses too much CPU
  -flagsAuthKey value
    	Auth key for /flags endpoint. It must be passed via authKey query arg. It overrides -httpAuth.*
    	Flag value can be read from the given file when using -flagsAuthKey=file:///abs/path/to/file or -flagsAuthKey=file://./relative/path/to/file.
    	Flag value can be read from the given http/https url when using -flagsAuthKey=http://host/path or -flagsAuthKey=https://host/path
  -fs.maxConcurrency int
    	The maximum number of concurrent goroutines to work with files; smaller values may help reducing Go scheduling latency on systems with small number of CPU cores; higher values may help reducing data ingestion latency on systems with high-latency storage such as NFS or Ceph (default fsutil.getDefaultConcurrency())
  -http.connTimeout duration
    	Incoming connections to -httpListenAddr are closed after the configured timeout. This may help evenly spreading load among a cluster of services behind TCP-level load balancer. Zero value disables closing of incoming connections (default 2m0s)
  -http.disableCORS
    	Disable CORS for all origins (*)
  -http.disableKeepAlive
    	Whether to disable HTTP keep-alive for incoming connections at -httpListenAddr
  -http.disableResponseCompression
    	Disable compression of HTTP responses to save CPU resources. By default, compression is enabled to save network bandwidth
  -http.header.csp string
    	Value for 'Content-Security-Policy' header, recommended: "default-src 'self'"
  -http.header.frameOptions string
    	Value for 'X-Frame-Options' header
  -http.header.hsts string
    	Value for 'Strict-Transport-Security' header, recommended: 'max-age=31536000; includeSubDomains'
  -http.idleConnTimeout duration
    	Timeout for incoming idle http connections (default 1m0s)
  -http.listenAddr string
    	http server listen addr (default ":8435")
  -http.maxGracefulShutdownDuration duration
    	The maximum duration for a graceful shutdown of the HTTP server. A highly loaded server may require increased value for a graceful shutdown (default 7s)
  -http.pathPrefix string
    	An optional prefix to add to all the paths handled by http server. For example, if '-http.pathPrefix=/foo/bar' is set, then all the http requests will be handled on '/foo/bar/*' paths. This may be useful for proxied requests. See https://www.robustperception.io/using-external-urls-and-proxies-with-prometheus
  -http.shutdownDelay duration
    	Optional delay before http server shutdown. During this delay, the server returns non-OK responses from /health page, so load balancers can route new requests to other servers
  -httpAuth.password value
    	Password for HTTP server's Basic Auth. The authentication is disabled if -httpAuth.username is empty
    	Flag value can be read from the given file when using -httpAuth.password=file:///abs/path/to/file or -httpAuth.password=file://./relative/path/to/file.
    	Flag value can be read from the given http/https url when using -httpAuth.password=http://host/path or -httpAuth.password=https://host/path
  -httpAuth.username string
    	Username for HTTP server's Basic Auth. The authentication is disabled if empty. See also -httpAuth.password
  -internStringCacheExpireDuration duration
    	The expiry duration for caches for interned strings. See https://en.wikipedia.org/wiki/String_interning . See also -internStringMaxLen and -internStringDisableCache (default 6m0s)
  -internStringDisableCache
    	Whether to disable caches for interned strings. This may reduce memory usage at the cost of higher CPU usage. See https://en.wikipedia.org/wiki/String_interning . See also -internStringCacheExpireDuration and -internStringMaxLen
  -internStringMaxLen int
    	The maximum length for strings to intern. A lower limit may save memory at the cost of higher CPU usage. See https://en.wikipedia.org/wiki/String_interning . See also -internStringDisableCache and -internStringCacheExpireDuration (default 500)
  -loggerDisableTimestamps
    	Whether to disable writing timestamps in logs
  -loggerErrorsPerSecondLimit int
    	Per-second limit on the number of ERROR messages. If more than the given number of errors are emitted per second, the remaining errors are suppressed. Zero values disable the rate limit
  -loggerFormat string
    	Format for logs. Possible values: default, json (default "default")
  -loggerJSONFields string
    	Allows renaming fields in JSON formatted logs. Example: "ts:timestamp,msg:message" renames "ts" to "timestamp" and "msg" to "message". Supported fields: ts, level, caller, msg
  -loggerLevel string
    	Minimum level of errors to log. Possible values: INFO, WARN, ERROR, FATAL, PANIC (default "INFO")
  -loggerMaxArgLen int
    	The maximum length of a single logged argument. Longer arguments are replaced with 'arg_start..arg_end', where 'arg_start' and 'arg_end' is prefix and suffix of the arg with the length not exceeding -loggerMaxArgLen / 2 (default 5000)
  -loggerOutput string
    	Output for the logs. Supported values: stderr, stdout (default "stderr")
  -loggerTimezone string
    	Timezone to use for timestamps in logs. Timezone must be a valid IANA Time Zone. For example: America/New_York, Europe/Berlin, Etc/GMT+3 or Local (default "UTC")
  -loggerWarnsPerSecondLimit int
    	Per-second limit on the number of WARN messages. If more than the given number of warns are emitted per second, then the remaining warns are suppressed. Zero values disable the rate limit
  -memory.allowedBytes size
    	Allowed size of system memory VictoriaMetrics caches may occupy. This option overrides -memory.allowedPercent if set to a non-zero value. Too low a value may increase the cache miss rate usually resulting in higher CPU and disk IO usage. Too high a value may evict too much data from the OS page cache resulting in higher disk IO usage
    	Supports the following optional suffixes for size values: KB, MB, GB, TB, KiB, MiB, GiB, TiB (default 0)
  -memory.allowedPercent float
    	Allowed percent of system memory VictoriaMetrics caches may occupy. See also -memory.allowedBytes. Too low a value may increase cache miss rate usually resulting in higher CPU and disk IO usage. Too high a value may evict too much data from the OS page cache which will result in higher disk IO usage (default 60)
  -metrics.exposeMetadata
    	Whether to expose TYPE and HELP metadata at the /metrics page, which is exposed at -httpListenAddr . The metadata may be needed when the /metrics page is consumed by systems, which require this information. For example, Managed Prometheus in Google Cloud - https://cloud.google.com/stackdriver/docs/managed-prometheus/troubleshooting#missing-metric-type
  -metricsAuthKey value
    	Auth key for /metrics endpoint. It must be passed via authKey query arg. It overrides -httpAuth.*
    	Flag value can be read from the given file when using -metricsAuthKey=file:///abs/path/to/file or -metricsAuthKey=file://./relative/path/to/file.
    	Flag value can be read from the given http/https url when using -metricsAuthKey=http://host/path or -metricsAuthKey=https://host/path
  -only-init-config
    	enables will read config and write to config-envsubst-file once before exit
  -pprofAuthKey value
    	Auth key for /debug/pprof/* endpoints. It must be passed via authKey query arg. It overrides -httpAuth.*
    	Flag value can be read from the given file when using -pprofAuthKey=file:///abs/path/to/file or -pprofAuthKey=file://./relative/path/to/file.
    	Flag value can be read from the given http/https url when using -pprofAuthKey=http://host/path or -pprofAuthKey=https://host/path
  -reload-url string
    	reload URL to trigger config reload (default "http://127.0.0.1:8429/-/reload")
  -reload-url-auth-key value
    	authKey for config reload API requests
    	Flag value can be read from the given file when using -reload-url-auth-key=file:///abs/path/to/file or -reload-url-auth-key=file://./relative/path/to/file.
    	Flag value can be read from the given http/https url when using -reload-url-auth-key=http://host/path or -reload-url-auth-key=https://host/path
  -reload-use-proxy-protocol
    	enables proxy-protocol for reload connections.
  -reload.tlsCAFile string
    	Optional path to client-side TLS CA file to use when connecting to -reload-url
  -reload.tlsCertFile string
    	Optional path to client-side TLS certificate file to use when connecting to -reload-url
  -reload.tlsInsecureSkipVerify
    	Whether to skip tls verification when connecting to -reload-url (default true)
  -reload.tlsKeyFile string
    	Optional path to client-side TLS key file to use when connecting to -reload-url
  -reload.tlsServerName string
    	Optional TLS server name to use for connections to -reload-url.
  -resync-interval duration
    	interval for force resync of the last configuration
  -rules-dir array
    	the same as watched-dir, legacy
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -secret.flags array
    	Comma-separated list of flag names with secret values. Values for these flags are hidden in logs and on /metrics page
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -tls array
    	Whether to enable TLS for incoming HTTP requests at the given -httpListenAddr (aka https). -tlsCertFile and -tlsKeyFile must be set if -tls is set. See also -mtls
    	Supports array of values separated by comma or specified via multiple flags.
    	Empty values are set to false.
  -tlsCertFile array
    	Path to file with TLS certificate for the corresponding -httpListenAddr if -tls is set. Prefer ECDSA certs instead of RSA certs as RSA certs are slower. The provided certificate file is automatically re-read every second, so it can be dynamically updated. See also -tlsAutocertHosts
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -tlsCipherSuites array
    	Optional list of TLS cipher suites for incoming requests over HTTPS if -tls is set. See the list of supported cipher suites at https://pkg.go.dev/crypto/tls#pkg-constants
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -tlsKeyFile array
    	Path to file with TLS key for the corresponding -httpListenAddr if -tls is set. The provided key file is automatically re-read every second, so it can be dynamically updated. See also -tlsAutocertHosts
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -tlsMinVersion array
    	Optional minimum TLS version to use for the corresponding -httpListenAddr if -tls is set. Supported values: TLS10, TLS11, TLS12, TLS13
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -version
    	Show VictoriaMetrics version
  -watch-interval duration
    	no-op for prometheus config-reloader compatibility (default 3m0s)
  -watched-dir array
    	directory to watch non-recursively
    	Supports an array of values separated by comma or specified via multiple flags.
    	Each array item can contain comma inside single-quoted or double-quoted string, {}, [] and () braces.
  -webhook-method string
    	the HTTP method url to use to send the webhook (default "GET")
```
