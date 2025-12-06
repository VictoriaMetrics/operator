package vmanomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"

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
	models, err := config.SelectModels(ctx, rclient, cr)
	if err != nil {
		return "", fmt.Errorf("selecting VMAnomalyModels failed: %w", err)
	}
	schedulers, err := config.SelectSchedulers(ctx, rclient, cr)
	if err != nil {
		return "", fmt.Errorf("selecting VMAnomalySchedulers failed: %w", err)
	}
	objects := &config.ChildObjects{
		Models:     models,
		Schedulers: schedulers,
	}
	data, err := config.Load(cr, objects, ac)
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

	if err := updateStatusesForChildObjects(ctx, rclient, cr, objects, childObject); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}

func updateStatusesForChildObjects(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, objects *config.ChildObjects, childObject client.Object) error {
	parentObject := fmt.Sprintf("%s.%s.vmanomaly", cr.Name, cr.Namespace)
	if childObject != nil && !reflect.ValueOf(childObject).IsNil() {
		// fast path
		switch obj := childObject.(type) {
		case *vmv1.VMAnomalyModel:
			for _, o := range objects.Models {
				if o.Name == obj.Name && o.Namespace == obj.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1.VMAnomalyModel{o})
				}
			}
		case *vmv1.VMAnomalyScheduler:
			for _, o := range objects.Schedulers {
				if o.Name == obj.Name && o.Namespace == obj.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1.VMAnomalyScheduler{o})
				}
			}
		}
	}
	return nil
}
