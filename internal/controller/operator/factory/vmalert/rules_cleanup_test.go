package vmalert

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRuleConfigMapCleanup(t *testing.T) {
	type opts struct {
		keep bool
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cr := &vmv1beta1.VMAlert{
			TypeMeta:   metav1.TypeMeta{APIVersion: vmv1beta1.SchemeGroupVersion.String(), Kind: "VMAlert"},
			ObjectMeta: metav1.ObjectMeta{Name: "rules", Namespace: "default", UID: "rules-owner"},
			Spec: vmv1beta1.VMAlertSpec{
				Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://datasource"},
			},
		}
		cms, err := makeRulesConfigMaps(cr, []vmv1beta1.RuleGroup{{Name: "same-name"}, {Name: "same-name"}})
		require.NoError(t, err)
		require.Len(t, cms, 2)
		foreign := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "external", Namespace: cr.Namespace, Labels: cr.FinalLabels(),
		}}
		objects := []runtime.Object{cr, foreign}
		for i := range cms {
			cms[i].Finalizers = []string{vmv1beta1.FinalizerName}
			objects = append(objects, &cms[i])
		}
		fclient := k8stools.GetTestClientWithObjects(objects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		var keepNames []string
		if o.keep {
			keepNames = []string{cms[0].Name}
		}
		require.NoError(t, CreateOrUpdate(ctx, cr, fclient, keepNames))
		for i := range cms {
			var got corev1.ConfigMap
			err := fclient.Get(ctx, client.ObjectKeyFromObject(&cms[i]), &got)
			if i == 0 && o.keep {
				require.NoError(t, err)
				require.Equal(t, cms[i].BinaryData, got.BinaryData)
			} else {
				require.True(t, k8serrors.IsNotFound(err), "configmap %s: %v", cms[i].Name, err)
			}
		}
		var got corev1.ConfigMap
		require.NoError(t, fclient.Get(ctx, client.ObjectKeyFromObject(foreign), &got))
	}
	f(opts{})
	f(opts{keep: true})
}
