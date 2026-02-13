package vmanomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly/config"
)

// createOrUpdateConfig reconcile configuration for vmanomaly and returns configuration consistent hash
func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, ac *build.AssetsCache) (string, error) {
	data, err := config.Load(cr, ac)
	if err != nil {
		return "", err
	}
	newSecretConfig := &corev1.Secret{
		ObjectMeta: build.ResourceMeta(build.SecretConfigResourceKind, cr),
		Data: map[string][]byte{
			secretConfigKey: data,
		},
	}
	owner := cr.AsOwner()
	for kind, secret := range ac.GetOutput() {
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta, &owner); err != nil {
			return "", err
		}
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(build.ResourceMeta(build.SecretConfigResourceKind, prevCR))
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig, prevSecretMeta, &owner); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}
