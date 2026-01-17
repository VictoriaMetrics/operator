package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestValidateVMClusterRefOrSpec_Matrix(t *testing.T) {
	ok := vmv1alpha1.VMClusterRefOrSpec{Ref: &corev1.LocalObjectReference{Name: "a"}}
	assert.NoError(t, validateVMClusterRefOrSpec(0, ok))

	ok2 := vmv1alpha1.VMClusterRefOrSpec{Name: "b", Spec: &vmv1beta1.VMClusterSpec{}}
	assert.NoError(t, validateVMClusterRefOrSpec(1, ok2))

	both := vmv1alpha1.VMClusterRefOrSpec{
		Name: "c",
		Ref:  &corev1.LocalObjectReference{Name: "c"},
		Spec: &vmv1beta1.VMClusterSpec{},
	}
	assert.Error(t, validateVMClusterRefOrSpec(2, both))

	none := vmv1alpha1.VMClusterRefOrSpec{}
	assert.Error(t, validateVMClusterRefOrSpec(3, none))

	missingRefName := vmv1alpha1.VMClusterRefOrSpec{Ref: &corev1.LocalObjectReference{}}
	assert.Error(t, validateVMClusterRefOrSpec(4, missingRefName))

	missingSpecName := vmv1alpha1.VMClusterRefOrSpec{Spec: &vmv1beta1.VMClusterSpec{}}
	assert.Error(t, validateVMClusterRefOrSpec(5, missingSpecName))

	// Spec with OverrideSpec simultaneously
	overrideJSON, _ := json.Marshal(vmv1beta1.VMClusterSpec{})
	bad := vmv1alpha1.VMClusterRefOrSpec{
		Name:         "x",
		Spec:         &vmv1beta1.VMClusterSpec{},
		OverrideSpec: &apiextensionsv1.JSON{Raw: overrideJSON},
	}
	assert.Error(t, validateVMClusterRefOrSpec(6, bad))
}
