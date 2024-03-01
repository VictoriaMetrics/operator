module github.com/VictoriaMetrics/operator

go 1.21

toolchain go1.22.0

require (
	github.com/VictoriaMetrics/VictoriaMetrics v1.97.1
	github.com/VictoriaMetrics/metricsql v0.72.1
	github.com/VictoriaMetrics/operator/api v0.0.0-20230626142506-8950675e8bb1
	github.com/fsnotify/fsnotify v1.7.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-logr/logr v1.4.1
	github.com/go-test/deep v1.0.7
	github.com/hashicorp/go-version v1.6.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/nsf/jsondiff v0.0.0-20230430225905-43f6cf3098c1
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.30.0
	github.com/pires/go-proxyproto v0.6.2
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.62.0
	github.com/prometheus-operator/prometheus-operator/pkg/client v0.62.0
	github.com/prometheus/client_golang v1.18.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.26.0
	golang.org/x/net v0.21.0
	golang.org/x/sync v0.6.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.29.0
	k8s.io/apiextensions-apiserver v0.29.0
	k8s.io/apimachinery v0.29.0
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog/v2 v2.110.1
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b
	sigs.k8s.io/controller-runtime v0.17.2
)

require (
	github.com/VictoriaMetrics/metrics v1.31.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.6.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.8.0 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.6 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.46.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fastjson v1.6.4 // indirect
	github.com/valyala/fastrand v1.1.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/valyala/histogram v1.2.0 // indirect
	github.com/valyala/quicktemplate v1.7.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20240119083558-1b970713d09a // indirect
	golang.org/x/oauth2 v0.17.0 // indirect
	golang.org/x/sys v0.17.0 // indirect
	golang.org/x/term v0.17.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.32.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/component-base v0.29.0 // indirect
	k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	github.com/VictoriaMetrics/operator/api => ./api
	k8s.io/api => k8s.io/api v0.27.11
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.27.11
	k8s.io/apimachinery => k8s.io/apimachinery v0.27.11
	k8s.io/client-go => k8s.io/client-go v0.27.11
	k8s.io/component-base => k8s.io/component-base v0.27.11
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20230501164219-8b0f38b5fd1f

)
