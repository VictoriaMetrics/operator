package watchnamespace

import (
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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
		&vmv1beta1.VMClusterList{},
		&vmv1beta1.VMAgentList{},
		&vmv1beta1.VMAlertList{},
		&vmv1beta1.VMAlertmanagerList{},
		&vmv1beta1.VMSingleList{},
		&vmv1beta1.VMUserList{},
	}

	JustBeforeEach(func() {
		objects := []client.Object{
			&vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmcluster",
				},
				Spec: vmv1beta1.VMClusterSpec{RetentionPeriod: "1"},
			},
			&vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmagent",
				},
				Spec: vmv1beta1.VMAgentSpec{
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://some-url"},
					},
				},
			},
			&vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmalert",
				},
				Spec: vmv1beta1.VMAlertSpec{
					Notifier:   &vmv1beta1.VMAlertNotifierSpec{URL: "http://some-notifier-url"},
					Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://some-single-url"},
				},
			},
			&vmv1beta1.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmalertmanager",
				},
			},
			&vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmsingle",
				},
				Spec: vmv1beta1.VMSingleSpec{
					RetentionPeriod: "1",
				},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "test-vmuser",
				},
				Spec: vmv1beta1.VMUserSpec{
					TargetRefs: []vmv1beta1.TargetRef{
						{
							Static: &vmv1beta1.StaticRef{
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
		processIdxSuffix := fmt.Sprintf("-%d", GinkgoParallelProcess())
		for _, object := range objects {
			object.SetName(object.GetName() + processIdxSuffix)
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
