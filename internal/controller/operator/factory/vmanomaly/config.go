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

	for kind, secret := range ac.GetOutput() {
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta); err != nil {
			return "", err
		}
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(build.ResourceMeta(build.SecretConfigResourceKind, prevCR))
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig, prevSecretMeta); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}

func Validate(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	if !cr.DeletionTimestamp.IsZero() {
		return nil
	}
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	ac := build.NewAssetsCache(ctx, rclient, cfg)
	_, err := config.Load(cr, ac)
	if err != nil {
		return err
	}
	return nil
}
