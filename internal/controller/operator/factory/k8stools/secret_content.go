package k8stools

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadOAuthSecrets fetches content of OAuth secret and returns it plain text value
func LoadOAuthSecrets(ctx context.Context, rclient client.Client, oauth2 *vmv1beta1.OAuth2, ns string, cache map[string]*corev1.Secret, cmCache map[string]*corev1.ConfigMap) (*OAuth2Creds, error) {
	var r OAuth2Creds
	if oauth2.ClientSecret != nil {
		s, err := GetCredFromSecret(ctx, rclient, ns, oauth2.ClientSecret, buildCacheKey(ns, oauth2.ClientSecret.Name), cache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s, err: %w", oauth2.ClientSecret.Name, err)
		}
		r.ClientSecret = s
	}
	if oauth2.ClientID.Secret != nil {
		s, err := GetCredFromSecret(ctx, rclient, ns, oauth2.ClientID.Secret, ns+"/"+oauth2.ClientID.Secret.Name, cache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s, err: %w", oauth2.ClientID.Secret, err)
		}
		r.ClientID = s
	} else if oauth2.ClientID.ConfigMap != nil {
		s, err := GetCredFromConfigMap(ctx, rclient, ns, *oauth2.ClientID.ConfigMap, buildCacheKey(ns, oauth2.ClientID.ConfigMap.Name), cmCache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s err: %w", oauth2.ClientID.ConfigMap.Name, err)
		}
		r.ClientID = s
	}

	return &r, nil
}

// LoadBasicAuthSecret fetch content of kubernetes secrets and returns it within plain text
func LoadBasicAuthSecret(ctx context.Context, rclient client.Client, ns string, basicAuth *vmv1beta1.BasicAuth, secretCache map[string]*corev1.Secret) (BasicAuthCreds, error) {
	var err error
	var bac BasicAuthCreds
	userNameContent, err := GetCredFromSecret(ctx, rclient, ns, &basicAuth.Username, fmt.Sprintf("%s/%s", ns, basicAuth.Username.Name), secretCache)
	if err != nil {
		return bac, err
	}
	bac.Username = userNameContent

	if len(basicAuth.Password.Name) == 0 {
		// fast path for empty password
		// it can be skipped or defined via password_file
		return bac, nil
	}
	passwordContent, err := GetCredFromSecret(ctx, rclient, ns, &basicAuth.Password, fmt.Sprintf("%s/%s", ns, basicAuth.Password.Name), secretCache)
	if err != nil {
		return bac, err
	}
	bac.Password = passwordContent
	return bac, nil
}

// GetCredFromSecret fetch content of secret by given key
func GetCredFromSecret(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel *corev1.SecretKeySelector,
	cacheKey string,
	cache map[string]*corev1.Secret,
) (string, error) {
	var s *corev1.Secret
	var ok bool
	if sel == nil {
		return "", fmt.Errorf("BUG, secret key selector must be non nil for cache key: %s, ns: %s", cacheKey, ns)
	}
	if s, ok = cache[cacheKey]; !ok {
		s = &corev1.Secret{}
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: sel.Name}, s); err != nil {
			return "", fmt.Errorf("unable to fetch key from secret: %q for object: %q : %w", sel.Name, cacheKey, err)
		}
		cache[cacheKey] = s
	}
	if s, ok := s.Data[sel.Key]; ok {
		return maybeTrimSpace(string(s)), nil
	}
	return "", &KeyNotFoundError{sel.Key, cacheKey, "secret"}
}

// GetCredFromConfigMap fetches content of configmap by given key
func GetCredFromConfigMap(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel corev1.ConfigMapKeySelector,
	cacheKey string,
	cache map[string]*corev1.ConfigMap,
) (string, error) {
	var s *corev1.ConfigMap
	var ok bool

	if s, ok = cache[cacheKey]; !ok {
		s = &corev1.ConfigMap{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: sel.Name}, s)
		if err != nil {
			return "", fmt.Errorf("cannot get configmap: %s at namespace %s, err: %s", sel.Name, ns, err)
		}
		cache[cacheKey] = s
	}

	if a, ok := s.Data[sel.Key]; ok {
		return maybeTrimSpace(a), nil
	}
	return "", &KeyNotFoundError{sel.Key, cacheKey, "configmap"}
}
