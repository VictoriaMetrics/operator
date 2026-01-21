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

// CreateOrUpdateConfig builds configuration for VMAnomaly
func CreateOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, childObject client.Object) error {
	var prevCR *vmv1.VMAnomaly
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	ac := getAssetsCache(ctx, rclient, cr)
	if _, err := createOrUpdateConfig(ctx, rclient, cr, prevCR, childObject, ac); err != nil {
		return err
	}
	return nil
}

// createOrUpdateConfig reconcile configuration for vmanomaly and returns configuration consistent hash
func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, childObject client.Object, ac *build.AssetsCache) (string, error) {
	pos, err := config.NewParsedObjects(ctx, rclient, cr)
	if err != nil {
		return "", err
	}
	data, err := pos.Load(cr, ac)
	if err != nil {
		return "", err
	}
	newSecretConfig := &corev1.Secret{
		ObjectMeta: build.ResourceMeta(build.SecretConfigResourceKind, cr),
		Data: map[string][]byte{
			configEnvsubstFilename: data,
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

	if err := pos.UpdateStatusesForChildObjects(ctx, rclient, cr, childObject); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}
