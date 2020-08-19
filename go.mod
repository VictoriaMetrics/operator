module github.com/VictoriaMetrics/operator

go 1.13

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/coreos/prometheus-operator v0.41.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-logr/logr v0.1.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20200625001655-4c5254603344
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/utils v0.0.0-20200603063816-c1c6865ac451
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/testing_frameworks v0.1.2 // indirect

)

// Pinned to kubernetes-1.18.6
replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/api => k8s.io/api v0.18.6
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.6
	k8s.io/client-go => k8s.io/client-go v0.18.6
)
