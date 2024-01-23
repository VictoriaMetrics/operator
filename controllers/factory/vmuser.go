package factory

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// builds vmauth config.
func buildVMAuthConfig(ctx context.Context, rclient client.Client, vmauth *victoriametricsv1beta1.VMAuth) ([]byte, error) {
	// fetch exist users for vmauth.
	users, err := selectVMUsers(ctx, vmauth, rclient)
	if err != nil {
		return nil, err
	}
	// sort for consistency.
	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})
	// check config for dups.
	if dup := isUsersUniq(users); len(dup) > 0 {
		return nil, fmt.Errorf("duplicate user name detected at VMAuth config: %q", strings.Join(dup, ","))
	}

	// loads info about exist operator object kind for crdRef.
	crdCache, err := FetchCRDCache(ctx, rclient, users)
	if err != nil {
		return nil, err
	}

	secretValueByRef, err := fetchVMUserSecretCacheByRef(ctx, rclient, users)
	if err != nil {
		return nil, err
	}

	// inject passwordRef secrets
	injectSecretValueByRef(users, secretValueByRef)
	// select secrets with user auth settings.
	toCreateSecrets, existSecrets, err := selectVMUserSecrets(ctx, rclient, users)
	if err != nil {
		return nil, err
	}

	// inject data from exist secrets into vmuser.spec if needed.
	toUpdate := injectAuthSettings(existSecrets, users)
	log.Info("VMAuth reconcile stats", "VMAuth", vmauth.Name, "toUpdate", len(toUpdate), "tocreate", len(toCreateSecrets), "exist", len(existSecrets))

	// generate yaml config for vmauth.
	cfg, err := generateVMAuthConfig(vmauth, users, crdCache)
	if err != nil {
		return nil, err
	}

	// inject generated password into secrets, that we want to create.
	toCreateSecrets = addCredentialsToCreateSecrets(users, toCreateSecrets)
	if err := createVMUserSecrets(ctx, rclient, toCreateSecrets); err != nil {
		return nil, err
	}
	// update secrets.
	// todo, probably, its better to reconcile it with finalizers merge and etc.
	for i := range toUpdate {
		secret := &toUpdate[i]
		if err := rclient.Update(ctx, secret); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func addCredentialsToCreateSecrets(src []*victoriametricsv1beta1.VMUser, dst []corev1.Secret) []corev1.Secret {
	for i := range dst {
		secret := &dst[i]
		for j := range src {
			user := src[j]
			if user.SecretName() != secret.Name {
				continue
			}
			// need to fill password/username
			if user.Spec.BearerToken != nil {
				continue
			}
			if user.Spec.Name != nil {
				secret.Data["name"] = []byte(*user.Spec.Name)
			}
			if user.Spec.UserName != nil {
				secret.Data["username"] = []byte(*user.Spec.UserName)
			}
			if user.Spec.Password != nil {
				secret.Data["password"] = []byte(*user.Spec.Password)
			}
		}
	}
	return dst
}

func createVMUserSecrets(ctx context.Context, rclient client.Client, secrets []corev1.Secret) error {
	for i := range secrets {
		secret := secrets[i]
		if err := rclient.Create(ctx, &secret); err != nil {
			return err
		}
	}
	return nil
}

func injectSecretValueByRef(src []*victoriametricsv1beta1.VMUser, secretValueCacheByRef map[string]string) {
	for i := range src {
		user := src[i]
		switch {
		case user.Spec.PasswordRef != nil:
			secretPassword := secretValueCacheByRef[user.PasswordRefAsKey()]
			user.Spec.Password = pointer.StringPtr(secretPassword)
		case user.Spec.TokenRef != nil:
			secretToken := secretValueCacheByRef[user.TokenRefAsKey()]
			user.Spec.BearerToken = pointer.StringPtr(secretToken)
		}

	}
}

func injectAuthSettings(src []corev1.Secret, dst []*victoriametricsv1beta1.VMUser) []corev1.Secret {
	var toUpdate []corev1.Secret
	if len(src) == 0 || len(dst) == 0 {
		return nil
	}
	for i := range src {
		secret := src[i]
		for j := range dst {
			vmuser := dst[j]
			if vmuser.SecretName() != secret.Name {
				continue
			}
			// check if secretUpdate needed.
			var needUpdate bool
			if vmuser.Spec.Name != nil {
				if string(secret.Data["name"]) != *vmuser.Spec.Name {
					needUpdate = true
					secret.Data["name"] = []byte(*vmuser.Spec.Name)
				}
			}

			if vmuser.Spec.BearerToken != nil {

				if len(secret.Data["username"]) > 0 || len(secret.Data["password"]) > 0 {
					needUpdate = true
					delete(secret.Data, "username")
					delete(secret.Data, "password")
				}
				if string(secret.Data["bearerToken"]) != *vmuser.Spec.BearerToken {
					needUpdate = true
					secret.Data["bearerToken"] = []byte(*vmuser.Spec.BearerToken)
				}
				if needUpdate {
					toUpdate = append(toUpdate, secret)
				}
				continue
			}
			existUser := secret.Data["username"]

			if vmuser.Spec.UserName == nil {
				vmuser.Spec.UserName = pointer.StringPtr(string(existUser))
				needUpdate = true
			} else if string(existUser) != *vmuser.Spec.UserName {
				secret.Data["username"] = []byte(*vmuser.Spec.UserName)
				needUpdate = true
			}

			existPassword := secret.Data["password"]

			// add previously generated password.
			if vmuser.Spec.GeneratePassword && vmuser.Spec.Password == nil {
				vmuser.Spec.Password = pointer.StringPtr(string(existPassword))
			} else if vmuser.Spec.Password != nil && string(existPassword) != *vmuser.Spec.Password {
				needUpdate = true
				secret.Data["password"] = []byte(*vmuser.Spec.Password)
			}

			if needUpdate {
				toUpdate = append(toUpdate, secret)
			}
		}
	}
	return toUpdate
}

func isUsersUniq(users []*victoriametricsv1beta1.VMUser) []string {
	uniq := make(map[string]struct{}, len(users))
	var dupUsers []string
	for i := range users {
		user := users[i]
		userName := user.Name
		if user.Spec.Name != nil {
			userName = *user.Spec.Name
		}
		// its ok to override userName, in this case it must be nil.
		if user.Spec.BearerToken != nil {
			userName = *user.Spec.BearerToken
		}
		if _, ok := uniq[userName]; ok {
			dupUsers = append(dupUsers, userName)
			continue
		}
		uniq[userName] = struct{}{}
	}
	return dupUsers
}

// builds configuration part for vmauth from given vmusers

type objectWithUrl interface {
	client.Object
	AsURL() string
}

func getAsURLObject(ctx context.Context, rclient client.Client, obj objectWithUrl) (string, error) {
	if err := rclient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj); err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return obj.AsURL(), nil
}

func fetchVMUserSecretCacheByRef(ctx context.Context, rclient client.Client, users []*victoriametricsv1beta1.VMUser) (map[string]string, error) {
	passwordCache := make(map[string]string, len(users))
	var fetchSecret corev1.Secret
	for i := range users {
		user := users[i]
		if user.Spec.DisableSecretCreation {
			continue
		}
		var secretName, secretKey, refValue string
		switch {
		case user.Spec.PasswordRef != nil:
			secretName = user.Spec.PasswordRef.Name
			secretKey = user.Spec.PasswordRef.Key
			refValue = user.PasswordRefAsKey()
		case user.Spec.TokenRef != nil:
			secretName = user.Spec.TokenRef.Name
			secretKey = user.Spec.TokenRef.Key
			refValue = user.TokenRefAsKey()
		default:
			continue
		}
		if _, ok := passwordCache[refValue]; ok {
			continue
		}
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: user.Namespace, Name: secretName}, &fetchSecret); err != nil {
			return nil, fmt.Errorf("cannot get secret to fetch value by ref for user: %s, at namespace: %s, err: %s", user.Name, user.Namespace, err)
		}
		secretValue := fetchSecret.Data[secretKey]
		if len(secretValue) == 0 {
			return nil, fmt.Errorf("cannot find ref value key: %s for user: %s, at namespace: %s", refValue, user.Name, user.Namespace)
		}
		passwordCache[refValue] = string(secretValue)
	}
	return passwordCache, nil
}

func FetchCRDCache(ctx context.Context, rclient client.Client, users []*victoriametricsv1beta1.VMUser) (map[string]string, error) {
	crdCacheUrlCache := make(map[string]string)
	for i := range users {
		user := users[i]
		for j := range user.Spec.TargetRefs {
			ref := user.Spec.TargetRefs[j]
			if ref.CRD == nil {
				continue
			}
			// namespace mismatch
			if !config.IsClusterWideAccessAllowed() && ref.CRD.Namespace != config.MustGetWatchNamespace() {
				log.Info("cannot discover CRD component on whole kubernetes cluster. Operator started with single namespace", "watch_namespace", config.MustGetWatchNamespace(), "crd_ref_name", ref.CRD.Name, "crd_ref_namespace", ref.CRD.Namespace)
				continue
			}
			if _, ok := crdCacheUrlCache[ref.CRD.AsKey()]; ok {
				continue
			}
			switch name := ref.CRD.Kind; name {
			case "VMAgent":
				var crd victoriametricsv1beta1.VMAgent
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlert":
				var crd victoriametricsv1beta1.VMAlert
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMSingle":
				var crd victoriametricsv1beta1.VMSingle
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlertmanager":
				var crd victoriametricsv1beta1.VMAlertmanager
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage":
				var crd victoriametricsv1beta1.VMCluster
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				if url == "" {
					continue
				}
				var targetURL string
				switch {
				case strings.HasSuffix(name, "vmselect"):
					targetURL = crd.VMSelectURL()
				case strings.HasSuffix(name, "vminsert"):
					targetURL = crd.VMInsertURL()
				case strings.HasSuffix(name, "vmstorage"):
					targetURL = crd.VMStorageURL()
				default:
					log.Error(fmt.Errorf("unsupported kind for VMCluster: %s", name), "cannot select crd ref")
					continue
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = targetURL
			default:
				log.Error(fmt.Errorf("unsupported kind: %s", name), "cannot select crd ref")
				continue
			}
		}
	}
	return crdCacheUrlCache, nil
}

// generateVMAuthConfig create VMAuth cfg for given Users.
func generateVMAuthConfig(cr *victoriametricsv1beta1.VMAuth, users []*victoriametricsv1beta1.VMUser, crdCache map[string]string) ([]byte, error) {
	var cfg yaml.MapSlice

	var cfgUsers []yaml.MapSlice
	for i := range users {
		user := users[i]
		userCfg, err := genUserCfg(user, crdCache)
		if err != nil {
			return nil, err
		}
		cfgUsers = append(cfgUsers, userCfg)
	}
	if len(cfgUsers) == 0 {
		log.Info("cannot find any user configuration for vmauth, using default")
		cfgUsers = append(cfgUsers, yaml.MapSlice{
			{
				Key:   "url_prefix",
				Value: "http://localhost:8428",
			},
			{
				Key:   "name",
				Value: "default-user",
			},
			{
				Key:   "bearer_token",
				Value: "some-default-token",
			},
		})
	}

	cfg = yaml.MapSlice{
		{
			Key:   "users",
			Value: cfgUsers,
		},
	}
	var unAuthorizedAccess []yaml.MapSlice
	for _, uc := range cr.Spec.UnauthorizedAccessConfig {
		urlMap := yaml.MapSlice{
			{
				Key:   "url_prefix",
				Value: uc.URLs,
			},
			{
				Key:   "src_paths",
				Value: uc.Paths,
			},
		}
		unAuthorizedAccess = append(unAuthorizedAccess, addIPFiltersToYaml(urlMap, uc.IPFilters))
	}
	if len(unAuthorizedAccess) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "unauthorized_user", Value: yaml.MapSlice{{Key: "url_map", Value: unAuthorizedAccess}}})
	}
	return yaml.Marshal(cfg)
}

// AddToYaml conditionally adds ip filters to dst yaml
func addIPFiltersToYaml(dst yaml.MapSlice, ipf victoriametricsv1beta1.VMUserIPFilters) yaml.MapSlice {
	ipFilters := yaml.MapSlice{}
	if len(ipf.AllowList) > 0 {
		ipFilters = append(ipFilters, yaml.MapItem{
			Key:   "allow_list",
			Value: ipf.AllowList,
		})
	}
	if len(ipf.DenyList) > 0 {
		ipFilters = append(ipFilters, yaml.MapItem{
			Key:   "deny_list",
			Value: ipf.DenyList,
		})
	}
	if len(ipFilters) > 0 {
		dst = append(dst, yaml.MapItem{Key: "ip_filters", Value: ipFilters})
	}
	return dst
}

// generates routing config for given target refs
func genUrlMaps(userName string, refs []victoriametricsv1beta1.TargetRef, result yaml.MapSlice, crdUrlCache map[string]string) (yaml.MapSlice, error) {
	var urlMaps []yaml.MapSlice
	handleRef := func(ref victoriametricsv1beta1.TargetRef) ([]string, error) {
		var urlPrefixes []string
		switch {
		case ref.CRD != nil:
			urlPrefix := crdUrlCache[ref.CRD.AsKey()]
			if urlPrefix == "" {
				return nil, fmt.Errorf("cannot find crdRef target: %q, for user: %s", ref.CRD.AsKey(), userName)
			}
			urlPrefixes = append(urlPrefixes, urlPrefix)
		case len(ref.Static.URL) > 0:
			urlPrefixes = append(urlPrefixes, ref.Static.URL)
		case len(ref.Static.URLs) > 0:
			urlPrefixes = ref.Static.URLs
		default:
			return nil, fmt.Errorf("static.url, static.urls and ref.crd cannot be empty for user: %s", userName)
		}

		if ref.TargetPathSuffix != "" {
			parsedSuffix, err := url.Parse(ref.TargetPathSuffix)
			if err != nil {
				return nil, fmt.Errorf("cannot parse targetPath: %q, err: %w", ref.TargetPathSuffix, err)
			}
			for idx, urlPrefix := range urlPrefixes {
				parsedUrlPrefix, err := url.Parse(urlPrefix)
				if err != nil {
					return nil, fmt.Errorf("cannot parse urlPrefix: %q,err: %w", urlPrefix, err)
				}
				parsedUrlPrefix.Path = path.Join(parsedUrlPrefix.Path, parsedSuffix.Path)
				suffixQuery := parsedSuffix.Query()
				// update query params if needed.
				if len(suffixQuery) > 0 {
					urlQ := parsedUrlPrefix.Query()
					for k, v := range suffixQuery {
						urlQ[k] = v
					}
					parsedUrlPrefix.RawQuery = urlQ.Encode()
				}
				urlPrefixes[idx] = parsedUrlPrefix.String()
			}
		}
		return urlPrefixes, nil
	}
	// fast path for single or empty route
	if len(refs) == 1 && len(refs[0].Paths) < 2 {
		srcPaths := refs[0].Paths
		var isDefaultRoute bool
		switch len(srcPaths) {
		case 0:
			// default route to everything
			isDefaultRoute = true
		case 1:
			// probably default route
			switch srcPaths[0] {
			case "/", "/*", "/.*":
				isDefaultRoute = true
			}
		}
		// special case, use different config syntax.
		if isDefaultRoute {
			ref := refs[0]
			urlPrefix, err := handleRef(ref)
			if err != nil {
				return result, fmt.Errorf("cannot build urlPrefix for one ref, err: %w", err)
			}
			result = append(result, yaml.MapItem{Key: "url_prefix", Value: urlPrefix})
			if len(ref.Headers) > 0 {
				result = append(result, yaml.MapItem{Key: "headers", Value: ref.Headers})
			}
			if len(ref.ResponseHeaders) > 0 {
				result = append(result, yaml.MapItem{Key: "response_headers", Value: ref.ResponseHeaders})
			}
			if len(ref.RetryStatusCodes) > 0 {
				result = append(result, yaml.MapItem{Key: "retry_status_codes", Value: ref.RetryStatusCodes})
			}
			if ref.DropSrcPathPrefixParts != nil {
				result = append(result, yaml.MapItem{Key: "drop_src_path_prefix_parts", Value: ref.DropSrcPathPrefixParts})
			}
			if ref.LoadBalancingPolicy != nil {
				result = append(result, yaml.MapItem{Key: "load_balancing_policy", Value: ref.LoadBalancingPolicy})
			}
			return result, nil
		}

	}

	for i := range refs {
		var urlMap yaml.MapSlice
		ref := refs[i]
		if ref.Static == nil && ref.CRD == nil {
			continue
		}
		urlPrefix, err := handleRef(ref)
		if err != nil {
			return result, err
		}

		paths := ref.Paths
		switch len(paths) {
		case 0:
			// special case for
			// https://github.com/VictoriaMetrics/operator/issues/379
			switch {
			case len(refs) > 1 && ref.CRD != nil && ref.CRD.Kind == "VMCluster/vminsert":
				paths = addVMInsertPaths(paths)
			case len(refs) > 1 && ref.CRD != nil && ref.CRD.Kind == "VMCluster/vmselect":
				paths = addVMSelectPaths(paths)
			default:
				paths = append(paths, "/.*")
			}

		case 1:
			switch paths[0] {
			case "/", "/*":
				paths = []string{"/.*"}
			}
		default:
		}

		urlMap = append(urlMap, yaml.MapItem{
			Key:   "url_prefix",
			Value: urlPrefix,
		})
		urlMap = append(urlMap, yaml.MapItem{
			Key:   "src_paths",
			Value: paths,
		})
		if len(ref.Headers) > 0 {
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "headers",
				Value: ref.Headers,
			})
		}
		if len(ref.ResponseHeaders) > 0 {
			urlMap = append(urlMap, yaml.MapItem{Key: "response_headers", Value: ref.ResponseHeaders})
		}
		if len(ref.RetryStatusCodes) > 0 {
			urlMap = append(urlMap, yaml.MapItem{Key: "retry_status_codes", Value: ref.RetryStatusCodes})
		}
		if ref.DropSrcPathPrefixParts != nil {
			urlMap = append(urlMap, yaml.MapItem{Key: "drop_src_path_prefix_parts", Value: ref.DropSrcPathPrefixParts})
		}
		if ref.LoadBalancingPolicy != nil {
			urlMap = append(urlMap, yaml.MapItem{Key: "load_balancing_policy", Value: ref.LoadBalancingPolicy})
		}
		urlMaps = append(urlMaps, urlMap)
	}
	result = append(result, yaml.MapItem{Key: "url_map", Value: urlMaps})
	return result, nil
}

// this function mutates user and fills missing fields,
// such password or username.
func genUserCfg(user *victoriametricsv1beta1.VMUser, crdUrlCache map[string]string) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	r, err := genUrlMaps(user.Name, user.Spec.TargetRefs, r, crdUrlCache)
	if err != nil {
		return nil, fmt.Errorf("cannot generate urlMaps for user: %w", err)
	}

	if len(user.Spec.DefaultURLs) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "default_url",
			Value: user.Spec.DefaultURLs,
		})
	}
	// generate user access config.
	var name, username, password, token string
	if user.Spec.Name != nil {
		name = *user.Spec.Name
	}
	if name != "" {
		r = append(r, yaml.MapItem{
			Key:   "name",
			Value: name,
		})
	}

	if user.Spec.UserName != nil {
		username = *user.Spec.UserName
	}
	if user.Spec.Password != nil {
		password = *user.Spec.Password
	}

	if user.Spec.BearerToken != nil {
		token = *user.Spec.BearerToken
	}
	if user.Spec.MaxConcurrentRequests != nil {
		r = append(r, yaml.MapItem{
			Key:   "max_concurrent_requests",
			Value: *user.Spec.MaxConcurrentRequests,
		})
	}
	if user.Spec.LoadBalancingPolicy != nil {
		r = append(r, yaml.MapItem{
			Key:   "load_balancing_policy",
			Value: *user.Spec.LoadBalancingPolicy,
		})
	}
	if user.Spec.DropSrcPathPrefixParts != nil {
		r = append(r, yaml.MapItem{
			Key:   "drop_src_path_prefix_parts",
			Value: *user.Spec.DropSrcPathPrefixParts,
		})
	}
	if user.Spec.TLSInsecureSkipVerify {
		r = append(r, yaml.MapItem{
			Key:   "tls_insecure_skip_verify",
			Value: user.Spec.TLSInsecureSkipVerify,
		})
	}
	if len(user.Spec.MetricLabels) != 0 {
		r = append(r, yaml.MapItem{
			Key:   "metric_labels",
			Value: user.Spec.MetricLabels,
		})
	}
	if len(user.Spec.RetryStatusCodes) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "retry_status_codes",
			Value: user.Spec.RetryStatusCodes,
		})
	}
	if len(user.Spec.Headers) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "headers",
			Value: user.Spec.Headers,
		})
	}
	if len(user.Spec.ResponseHeaders) > 0 {
		r = append(r, yaml.MapItem{Key: "response_headers", Value: user.Spec.ResponseHeaders})
	}
	r = addIPFiltersToYaml(r, user.Spec.IPFilters)

	// fast path.
	if token != "" {
		r = append(r, yaml.MapItem{
			Key:   "bearer_token",
			Value: token,
		})
		return r, nil
	}
	// mutate vmuser
	if username == "" {
		username = user.Name
		user.Spec.UserName = pointer.StringPtr(username)
	}
	if user.Spec.GeneratePassword && password == "" {
		pwd, err := genPassword()
		if err != nil {
			return nil, err
		}
		password = pwd
		user.Spec.Password = pointer.StringPtr(password)
	}
	r = append(r, yaml.MapItem{
		Key:   "username",
		Value: username,
	})
	if password != "" {
		r = append(r, yaml.MapItem{
			Key:   "password",
			Value: password,
		})
	}

	return r, nil
}

// simple password generation.
// its kubernetes, strong security does not work there.
var (
	passwordLength = 10
	charSet        = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdedfghijklmnopqrst0123456789"
	maxIdx         = big.NewInt(int64(len(charSet)))
)

func genPassword() (string, error) {
	var dst strings.Builder
	for i := 0; i < passwordLength; i++ {
		r, err := rand.Int(rand.Reader, maxIdx)
		if err != nil {
			return "", err
		}
		dst.WriteRune(rune(charSet[r.Int64()]))
	}

	return dst.String(), nil
}

// selects vmusers for given vmauth.
func selectVMUsers(ctx context.Context, cr *victoriametricsv1beta1.VMAuth, rclient client.Client) ([]*victoriametricsv1beta1.VMUser, error) {
	var res []*victoriametricsv1beta1.VMUser
	namespaces, userSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.UserNamespaceSelector, cr.Spec.UserSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMUserList{}, userSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMUserList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			res = append(res, item.DeepCopy())
		}
	}); err != nil {
		return nil, err
	}

	var vmUsers []string
	for k := range res {
		vmUsers = append(vmUsers, res[k].Name)
	}
	log.Info("selected VMUsers", "vmusers", strings.Join(vmUsers, ","), "namespace", cr.Namespace, "vmauth", cr.Name)

	return res, nil
}

// select existing vmusers secrets.
// returns secrets, that need to be create and exist secrets.
func selectVMUserSecrets(ctx context.Context, rclient client.Client, vmUsers []*victoriametricsv1beta1.VMUser) ([]corev1.Secret, []corev1.Secret, error) {
	var existsSecrets []corev1.Secret
	var needToCreateSecrets []corev1.Secret
	for i := range vmUsers {
		vmUser := vmUsers[i]
		if vmUser.Spec.DisableSecretCreation {
			continue
		}
		var vmus corev1.Secret
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: vmUser.Namespace, Name: vmUser.SecretName()}, &vmus); err != nil {
			if errors.IsNotFound(err) {
				needToCreateSecrets = append(needToCreateSecrets, buildVMUserSecret(vmUser))
				continue
			}
			return nil, nil, fmt.Errorf("cannot query kubernetes api for vmuser secrets: %w", err)
		} else {
			existsSecrets = append(existsSecrets, vmus)
		}
	}
	return needToCreateSecrets, existsSecrets, nil
}

// note, username and password must be filled by operator
// with default values if need.
func buildVMUserSecret(src *victoriametricsv1beta1.VMUser) corev1.Secret {
	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            src.SecretName(),
			Namespace:       src.Namespace,
			Labels:          src.AllLabels(),
			Annotations:     src.AnnotationsFiltered(),
			OwnerReferences: src.AsOwner(),
			Finalizers: []string{
				victoriametricsv1beta1.FinalizerName,
			},
		},
		Data: map[string][]byte{},
	}
	if src.Spec.Name != nil {
		s.Data["name"] = []byte(*src.Spec.Name)
	}
	if src.Spec.BearerToken != nil {
		s.Data["bearerToken"] = []byte(*src.Spec.BearerToken)
	}
	if src.Spec.UserName != nil {
		s.Data["username"] = []byte(*src.Spec.UserName)
	}
	if src.Spec.Password != nil {
		s.Data["password"] = []byte(*src.Spec.Password)
	}
	return s
}

func addVMInsertPaths(src []string) []string {
	return append(src,
		"/newrelic/.*",
		"/opentelemetry/.*",
		"/prometheus/api/v1/write",
		"/prometheus/api/v1/import.*",
		"/influx/.*",
		"/datadog/.*")
}

func addVMSelectPaths(src []string) []string {
	return append(src, "/vmui.*",
		"/vmui/vmui",
		"/graph",
		"/prometheus/graph",
		"/prometheus/vmui.*",
		"/prometheus/api/v1/label.*",
		"/graphite.*",
		"/prometheus/api/v1/query.*",
		"/prometheus/api/v1/rules",
		"/prometheus/api/v1/alerts",
		"/prometheus/api/v1/metadata",
		"/prometheus/api/v1/rules",
		"/prometheus/api/v1/series.*",
		"/prometheus/api/v1/status.*",
		"/prometheus/api/v1/export.*",
		"/prometheus/federate",
		"/prometheus/api/v1/admin/tsdb/delete_series",
		"/admin/tenants",
		"/api/v1/status/.*",
		"/internal/resetRollupResultCache",
		"/prometheus/api/v1/admin/.*",
	)
}
