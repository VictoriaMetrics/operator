package fuzzutil

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type updateRecorder struct {
	admission.Validator[*corev1.ConfigMap]
	old, obj *corev1.ConfigMap
}

func (r *updateRecorder) ValidateCreate(context.Context, *corev1.ConfigMap) (admission.Warnings, error) {
	return nil, nil
}

func (r *updateRecorder) ValidateUpdate(_ context.Context, old, obj *corev1.ConfigMap) (admission.Warnings, error) {
	r.old, r.obj = old, obj
	return nil, nil
}

func (r *updateRecorder) ValidateDelete(context.Context, *corev1.ConfigMap) (admission.Warnings, error) {
	return nil, nil
}

func TestValidatorUpdateDiffers(t *testing.T) {
	r := &updateRecorder{}
	target := NewValidator(func() *corev1.ConfigMap { return &corev1.ConfigMap{} }, r)

	// malformed JSON takes the filler path for both objects
	target.run(t, []byte(strings.Repeat("not json\x01\xff", 50)))
	if cmp.Equal(r.old, r.obj) {
		t.Fatal("old and new objects are identical")
	}
}
