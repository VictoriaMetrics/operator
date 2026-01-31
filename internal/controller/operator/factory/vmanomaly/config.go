package vmanomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly/config"
)

// createOrUpdateConfig reconcile configuration for vmanomaly and returns configuration consistent hash
func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, ac *build.AssetsCache) (string, error) {
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

	for kind, secret := range ac.GetOutput() {
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		if err := reconcile.Secret(ctx, rclient, &secret); err != nil {
			return "", err
		}
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}
