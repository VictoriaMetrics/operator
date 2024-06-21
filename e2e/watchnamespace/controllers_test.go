package watchnamespace

import (
	"reflect"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// formattableType wraps reflect.Type to provide a friendly diagnostic when assertions on it fail
type formattableType struct {
	reflect.Type
}

func (ft formattableType) GomegaString() string {
	return ft.String()
}

var _ = Describe("Controllers", func() {
	var namespace string
	objectListProtos := []client.ObjectList{
		&v1beta1vm.VMClusterList{},
		&v1beta1vm.VMAgentList{},
		&v1beta1vm.VMAlertList{},
		&v1beta1vm.VMAlertmanagerList{},
		&v1beta1vm.VMSingleList{},
		&v1beta1vm.VMUserList{},
	}

	JustBeforeEach(func() {
		objects := []client.Object{
			&v1beta1vm.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmcluster",
				},
				Spec: v1beta1vm.VMClusterSpec{RetentionPeriod: "1"},
			},
			&v1beta1vm.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmagent",
				},
				Spec: v1beta1vm.VMAgentSpec{
					RemoteWrite: []v1beta1vm.VMAgentRemoteWriteSpec{
						{URL: "http://some-url"},
					},
				},
			},
			&v1beta1vm.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmalert",
				},
				Spec: v1beta1vm.VMAlertSpec{
					Notifier:   &v1beta1vm.VMAlertNotifierSpec{URL: "http://some-notifier-url"},
					Datasource: v1beta1vm.VMAlertDatasourceSpec{URL: "http://some-single-url"},
				},
			},
			&v1beta1vm.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmalertmanager",
				},
			},
			&v1beta1vm.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmsingle",
				},
				Spec: v1beta1vm.VMSingleSpec{
					RetentionPeriod: "1",
				},
			},
			&v1beta1vm.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmuser",
				},
				Spec: v1beta1vm.VMUserSpec{
					TargetRefs: []v1beta1vm.TargetRef{
						{
							Static: &v1beta1vm.StaticRef{
								URL: "http://vmselect",
							},
							Paths: []string{
								"/select/0/prometheus",
								"/select/0/graphite",
							},
						},
					},
				},
			},
		}

		var objectTypes []formattableType
		for _, object := range objects {
			objectTypes = append(objectTypes, formattableType{reflect.TypeOf(object).Elem()})
		}

		var listObjectTypes []formattableType
		for _, listProto := range objectListProtos {
			listObjectTypes = append(listObjectTypes, formattableType{GetListObjectType(listProto)})
		}

		// self check that we test all objects that we create
		Expect(objectTypes).To(Equal(listObjectTypes))

		CreateObjects(objects...)
	})

	AfterEach(func() {
		DeleteAllObjectsOf(namespace, objectListProtos...)
	})

	Context("when resources are inside WATCH_NAMESPACE", func() {
		BeforeEach(func() {
			namespace = includedNamespace
		})

		It("should add finalizers", func() {
			for _, listProto := range objectListProtos {
				EventuallyShouldHaveFinalizer(namespace, listProto)
			}
		})
	})

	Context("when resources are outside WATCH_NAMESPACE", func() {
		BeforeEach(func() {
			namespace = excludedNamespace
		})

		It("should NOT add finalizer", func() {
			ConsistentlyShouldNotHaveFinalizer(namespace, objectListProtos...)
		})
	})
})
