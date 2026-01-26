package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestValidateVMClusterObjOrRef_Matrix(t *testing.T) {
	s := VMClusterObjOrRef{Ref: &corev1.LocalObjectReference{Name: "a"}}
	assert.NoError(t, s.validate(nil))

	s2 := VMClusterObjOrRef{Name: "b", Spec: &vmv1beta1.VMClusterSpec{}}
	assert.NoError(t, s2.validate(nil))

	both := VMClusterObjOrRef{
		Name: "c",
		Ref:  &corev1.LocalObjectReference{Name: "c"},
		Spec: &vmv1beta1.VMClusterSpec{},
	}
	assert.Error(t, both.validate(nil))

	none := VMClusterObjOrRef{}
	assert.Error(t, none.validate(nil))

	missingRefName := VMClusterObjOrRef{Ref: &corev1.LocalObjectReference{}}
	assert.Error(t, missingRefName.validate(nil))

	missingSpecName := VMClusterObjOrRef{Spec: &vmv1beta1.VMClusterSpec{}}
	assert.Error(t, missingSpecName.validate(nil))
}
