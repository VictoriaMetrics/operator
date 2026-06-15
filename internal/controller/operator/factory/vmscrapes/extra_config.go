package vmscrapes

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

type extraConfigOwner interface {
	PrefixedName() string
	GetNamespace() string
	FinalLabels() map[string]string
	AsOwner() metav1.OwnerReference
}

// ExtraConfigSecretName returns the name of the Nth overflow secret for cr.
func ExtraConfigSecretName(cr extraConfigOwner, idx int) string {
	return fmt.Sprintf("%s-sc-%d", cr.PrefixedName(), idx)
}

// BuildExtraConfigSecret creates a Secret with gzip-compressed YAML for one overflow bucket.
func BuildExtraConfigSecret(cr extraConfigOwner, idx int, data []byte) *corev1.Secret {
	owner := cr.AsOwner()
	lbls := cr.FinalLabels()
	lbls[ExtraConfigLabelKey] = cr.PrefixedName()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ExtraConfigSecretName(cr, idx),
			Namespace:       cr.GetNamespace(),
			Labels:          lbls,
			Annotations:     map[string]string{"generated": "true"},
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Data: map[string][]byte{
			ExtraConfigFilename: data,
		},
	}
}

// RemoveStaleExtraConfigSecrets deletes overflow Secrets with index > activeCount.
func RemoveStaleExtraConfigSecrets(ctx context.Context, rclient client.Client, cr extraConfigOwner, activeCount int) error {
	var list corev1.SecretList
	if err := rclient.List(ctx, &list,
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels{ExtraConfigLabelKey: cr.PrefixedName()},
	); err != nil {
		return fmt.Errorf("listing stale extra scrape config secrets: %w", err)
	}
	activePrefix := cr.PrefixedName() + "-sc-"
	for i := range list.Items {
		s := &list.Items[i]
		if strings.HasPrefix(s.Name, activePrefix) {
			if idx, err := strconv.Atoi(s.Name[len(activePrefix):]); err == nil && idx <= activeCount {
				continue
			}
		}
		if err := finalize.SafeDelete(ctx, rclient, s); err != nil {
			return err
		}
	}
	return nil
}

// Shared constants for overflow scrape config Secrets (VMAgent and VMSingle).
const (
	// ExtraConfigFilename is the key in each overflow Secret holding the gzip-compressed job list.
	ExtraConfigFilename = "jobs.yaml"
	// ExtraConfigLabelKey is set on overflow Secrets for label-selector-based cleanup.
	ExtraConfigLabelKey = "operator.victoriametrics.com/scrape-config-extra"
	// ExtraConfigOutDir is where the config-reloader writes decompressed extra job files.
	ExtraConfigOutDir = "/etc/vm/sc-files"
	// ExtraConfigRawDirFmt is the mount path format for read-only extra Secret volumes.
	ExtraConfigRawDirFmt = "/etc/vm/sc-raw-%d"
	// ExtraConfigFilesGlob is added to the main scrape config when overflow is active.
	ExtraConfigFilesGlob = "/etc/vm/sc-files/*/jobs.yaml"
)
