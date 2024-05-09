package vmauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	crdCache, err := FetchCRDRefURLs(ctx, rclient, users)
	if err != nil {
		return nil, err
	}

	toCreateSecrets, toUpdate, err := addAuthCredentialsBuildSecrets(ctx, rclient, users)
	if err != nil {
		return nil, err
	}

	// inject data from exist secrets into vmuser.spec if needed.
	// toUpdate := injectAuthSettings(existSecrets, users)

	// generate yaml config for vmauth.
	cfg, err := generateVMAuthConfig(vmauth, users, crdCache)
	if err != nil {
		return nil, err
	}

	// inject generated password into secrets, that we want to create.
	if err := createVMUserSecrets(ctx, rclient, toCreateSecrets); err != nil {
		return nil, err
	}
	// update secrets.
	// todo, probably, its better to reconcile it with finalizers merge and etc.
	for i := range toUpdate {
		secret := toUpdate[i]
		if err := rclient.Update(ctx, secret); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func createVMUserSecrets(ctx context.Context, rclient client.Client, secrets []*v1.Secret) error {
	for i := range secrets {
		secret := secrets[i]
		if err := rclient.Create(ctx, secret); err != nil {
			return err
		}
	}
	return nil
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

type objectWithURL interface {
	client.Object
	AsURL() string
}

func getAsURLObject(ctx context.Context, rclient client.Client, obj objectWithURL) (string, error) {
	if err := rclient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj); err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return obj.AsURL(), nil
}

func addAuthCredentialsBuildSecrets(ctx context.Context, rclient client.Client, users []*victoriametricsv1beta1.VMUser) (needToCreateSecrets []*v1.Secret, needToUpdateSecrets []*v1.Secret, err error) {
	dst := make(map[string]*v1.Secret)
	for i := range users {
		user := users[i]
		switch {
		case user.Spec.PasswordRef != nil:
			v, err := k8stools.GetCredFromSecret(ctx, rclient, user.Namespace, user.Spec.PasswordRef, fmt.Sprintf("%s/%s", user.Namespace, user.Spec.PasswordRef.Name), dst)
			if err != nil {
				return needToCreateSecrets, needToUpdateSecrets, err
			}
			user.Spec.Password = ptr.To(v)
		case user.Spec.TokenRef != nil:
			v, err := k8stools.GetCredFromSecret(ctx, rclient, user.Namespace, user.Spec.TokenRef, fmt.Sprintf("%s/%s", user.Namespace, user.Spec.TokenRef.Name), dst)
			if err != nil {
				return needToCreateSecrets, needToUpdateSecrets, err
			}
			user.Spec.BearerToken = ptr.To(v)
		}

		if !user.Spec.DisableSecretCreation {
			var vmus v1.Secret
			if err := rclient.Get(ctx, types.NamespacedName{Namespace: user.Namespace, Name: user.SecretName()}, &vmus); err != nil {
				if errors.IsNotFound(err) {
					userSecret, err := buildVMUserSecret(user)
					if err != nil {
						return needToCreateSecrets, needToUpdateSecrets, err
					}
					needToCreateSecrets = append(needToCreateSecrets, userSecret)
				} else {
					return needToCreateSecrets, needToUpdateSecrets, fmt.Errorf("cannot query kubernetes api for vmuser secrets: %w", err)
				}
			} else {
				// secret exists, check it's state
				if injectAuthSettings(&vmus, user) {
					needToUpdateSecrets = append(needToUpdateSecrets, &vmus)
				}
			}
		}
		if err := injectBackendAuthHeader(ctx, rclient, user, dst); err != nil {
			return needToCreateSecrets, needToUpdateSecrets, fmt.Errorf("cannot inject auth backend header into vmuser=%q: %w", user.Name, err)
		}

	}
	return
}

func injectBackendAuthHeader(ctx context.Context, rclient client.Client, user *victoriametricsv1beta1.VMUser, nsCache map[string]*v1.Secret) error {
	for j := range user.Spec.TargetRefs {
		ref := &user.Spec.TargetRefs[j]
		if ref.TargetRefBasicAuth != nil {
			bac, err := k8stools.LoadBasicAuthSecret(ctx, rclient,
				user.Namespace,
				&victoriametricsv1beta1.BasicAuth{
					Username: ref.TargetRefBasicAuth.Username,
					Password: ref.TargetRefBasicAuth.Password,
				}, nsCache,
			)
			if err != nil {
				return fmt.Errorf("could not load basicAuth config. %w", err)
			}
			token := bac.Username + ":" + bac.Password
			token64 := base64.StdEncoding.EncodeToString([]byte(token))
			Header := "Authorization: Basic " + token64
			ref.URLMapCommon.RequestHeaders = append(ref.URLMapCommon.RequestHeaders, Header)
		}
	}
	return nil
}

func injectAuthSettings(secret *v1.Secret, vmuser *victoriametricsv1beta1.VMUser) bool {
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
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
		return needUpdate
	}
	existUser := secret.Data["username"]

	if vmuser.Spec.UserName == nil {
		vmuser.Spec.UserName = ptr.To(string(existUser))
	} else if string(existUser) != *vmuser.Spec.UserName {
		secret.Data["username"] = []byte(*vmuser.Spec.UserName)
		needUpdate = true
	}

	existPassword := secret.Data["password"]
	// add previously generated password.
	if vmuser.Spec.GeneratePassword && vmuser.Spec.Password == nil {
		vmuser.Spec.Password = ptr.To(string(existPassword))
	} else if vmuser.Spec.Password != nil && string(existPassword) != *vmuser.Spec.Password {
		needUpdate = true
		secret.Data["password"] = []byte(*vmuser.Spec.Password)
	}
	return needUpdate
}

// FetchCRDRefURLs performs a fetch for CRD objects for vmauth users and returns an url by crd ref key name
func FetchCRDRefURLs(ctx context.Context, rclient client.Client, users []*victoriametricsv1beta1.VMUser) (map[string]string, error) {
	crdCacheURLCache := make(map[string]string)
	for i := range users {
		user := users[i]
		for j := range user.Spec.TargetRefs {
			ref := user.Spec.TargetRefs[j]
			if ref.CRD == nil {
				continue
			}
			if _, ok := crdCacheURLCache[ref.CRD.AsKey()]; ok {
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
				crdCacheURLCache[ref.CRD.AsKey()] = url
			case "VMAlert":
				var crd victoriametricsv1beta1.VMAlert
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheURLCache[ref.CRD.AsKey()] = url

			case "VMSingle":
				var crd victoriametricsv1beta1.VMSingle
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheURLCache[ref.CRD.AsKey()] = url
			case "VMAlertmanager":
				var crd victoriametricsv1beta1.VMAlertmanager
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheURLCache[ref.CRD.AsKey()] = url

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
					logger.WithContext(ctx).Error(fmt.Errorf("unsupported kind for VMCluster: %s", name), "cannot select crd ref")
					continue
				}
				crdCacheURLCache[ref.CRD.AsKey()] = targetURL
			default:
				logger.WithContext(ctx).Error(fmt.Errorf("unsupported kind: %s", name), "cannot select crd ref")
				continue
			}
		}
	}
	return crdCacheURLCache, nil
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
	if len(cfgUsers) > 0 {
		cfg = yaml.MapSlice{
			{
				Key:   "users",
				Value: cfgUsers,
			},
		}
	}

	var unAuthorizedAccess []yaml.MapSlice
	for _, uc := range cr.Spec.UnauthorizedAccessConfig {
		urlMap := appendIfNotNull(uc.SrcPaths, "src_paths", yaml.MapSlice{})
		urlMap = appendIfNotNull(uc.SrcHosts, "src_hosts", urlMap)
		urlMap = appendIfNotNull(uc.URLPrefix, "url_prefix", urlMap)

		urlMap = addURLMapCommonToYaml(urlMap, uc.URLMapCommon, false)
		unAuthorizedAccess = append(unAuthorizedAccess, urlMap)
	}
	var unAuthorizedAccessOpt []yaml.MapItem
	unAuthorizedAccessOpt = addUserConfigOptionToYaml(unAuthorizedAccessOpt, cr.Spec.UnauthorizedAccessConfigOption)

	var unAuthorizedAccessValue []yaml.MapItem
	if len(unAuthorizedAccess) > 0 {
		unAuthorizedAccessValue = append(unAuthorizedAccessValue, yaml.MapItem{Key: "url_map", Value: unAuthorizedAccess})
	}
	if len(unAuthorizedAccessOpt) > 0 {
		unAuthorizedAccessValue = append(unAuthorizedAccessValue, unAuthorizedAccessOpt...)
	}
	if len(unAuthorizedAccessValue) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "unauthorized_user", Value: unAuthorizedAccessValue})
	}
	return yaml.Marshal(cfg)
}

func appendIfNotNull(src []string, key string, origin yaml.MapSlice) yaml.MapSlice {
	if len(src) > 0 {
		return append(origin, yaml.MapItem{
			Key:   key,
			Value: src,
		})
	}
	return origin
}

func addURLMapCommonToYaml(dst yaml.MapSlice, opt victoriametricsv1beta1.URLMapCommon, isDefaultRoute bool) yaml.MapSlice {
	if !isDefaultRoute {
		dst = appendIfNotNull(opt.SrcQueryArgs, "src_query_args", dst)
		dst = appendIfNotNull(opt.SrcHeaders, "src_headers", dst)
	}
	dst = appendIfNotNull(opt.RequestHeaders, "headers", dst)
	dst = appendIfNotNull(opt.ResponseHeaders, "response_headers", dst)
	if opt.DiscoverBackendIPs != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "discover_backend_ips",
			Value: *opt.DiscoverBackendIPs,
		},
		)
	}
	if len(opt.RetryStatusCodes) > 0 {
		dst = append(dst, yaml.MapItem{
			Key:   "retry_status_codes",
			Value: opt.RetryStatusCodes,
		},
		)
	}
	if opt.MaxConcurrentRequests != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "max_concurrent_requests",
			Value: *opt.MaxConcurrentRequests,
		},
		)
	}
	if opt.LoadBalancingPolicy != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "load_balancing_policy",
			Value: *opt.LoadBalancingPolicy,
		},
		)
	}
	if opt.DropSrcPathPrefixParts != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "drop_src_path_prefix_parts",
			Value: *opt.DropSrcPathPrefixParts,
		},
		)
	}
	return addIPFiltersToYaml(dst, opt.IPFilters)
}

func addUserConfigOptionToYaml(dst yaml.MapSlice, opt *victoriametricsv1beta1.UserConfigOption) yaml.MapSlice {
	if opt == nil {
		return dst
	}
	if len(opt.DefaultURLs) > 0 {
		dst = append(dst, yaml.MapItem{Key: "default_url", Value: opt.DefaultURLs})
	}
	if opt.TLSCAFile != "" {
		dst = append(dst, yaml.MapItem{Key: "tls_ca_file", Value: opt.TLSCAFile})
	}
	if opt.TLSCertFile != "" {
		dst = append(dst, yaml.MapItem{Key: "tls_cert_file", Value: opt.TLSCertFile})
	}
	if opt.TLSKeyFile != "" {
		dst = append(dst, yaml.MapItem{Key: "tls_key_file", Value: opt.TLSKeyFile})
	}
	if opt.TLSServerName != "" {
		dst = append(dst, yaml.MapItem{Key: "tls_server_name", Value: opt.TLSServerName})
	}
	if opt.TLSInsecureSkipVerify != nil {
		dst = append(dst, yaml.MapItem{Key: "tls_insecure_skip_verify", Value: *opt.TLSInsecureSkipVerify})
	}
	dst = addIPFiltersToYaml(dst, opt.IPFilters)
	dst = appendIfNotNull(opt.Headers, "headers", dst)
	dst = appendIfNotNull(opt.ResponseHeaders, "response_headers", dst)
	if opt.DiscoverBackendIPs != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "discover_backend_ips",
			Value: *opt.DiscoverBackendIPs,
		},
		)
	}
	if len(opt.RetryStatusCodes) > 0 {
		dst = append(dst, yaml.MapItem{
			Key:   "retry_status_codes",
			Value: opt.RetryStatusCodes,
		},
		)
	}
	if opt.MaxConcurrentRequests != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "max_concurrent_requests",
			Value: *opt.MaxConcurrentRequests,
		},
		)
	}
	if opt.LoadBalancingPolicy != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "load_balancing_policy",
			Value: *opt.LoadBalancingPolicy,
		},
		)
	}
	if opt.DropSrcPathPrefixParts != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "drop_src_path_prefix_parts",
			Value: *opt.DropSrcPathPrefixParts,
		},
		)
	}
	return dst
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
func genURLMaps(userName string, refs []victoriametricsv1beta1.TargetRef, result yaml.MapSlice, crdURLCache map[string]string) (yaml.MapSlice, error) {
	var urlMaps []yaml.MapSlice
	handleRef := func(ref victoriametricsv1beta1.TargetRef) ([]string, error) {
		var urlPrefixes []string
		switch {
		case ref.CRD != nil:
			urlPrefix := crdURLCache[ref.CRD.AsKey()]
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
				parsedURLPrefix, err := url.Parse(urlPrefix)
				if err != nil {
					return nil, fmt.Errorf("cannot parse urlPrefix: %q,err: %w", urlPrefix, err)
				}
				parsedURLPrefix.Path = path.Join(parsedURLPrefix.Path, parsedSuffix.Path)
				suffixQuery := parsedSuffix.Query()
				// update query params if needed.
				if len(suffixQuery) > 0 {
					urlQ := parsedURLPrefix.Query()
					for k, v := range suffixQuery {
						urlQ[k] = v
					}
					parsedURLPrefix.RawQuery = urlQ.Encode()
				}
				urlPrefixes[idx] = parsedURLPrefix.String()
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
			result = addURLMapCommonToYaml(result, ref.URLMapCommon, isDefaultRoute)
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
		if len(ref.Hosts) > 0 {
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "src_hosts",
				Value: ref.Hosts,
			})
		}
		if len(ref.URLMapCommon.RequestHeaders) > 0 {
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "headers",
				Value: ref.URLMapCommon.RequestHeaders,
			})
		}
		if len(ref.URLMapCommon.ResponseHeaders) > 0 {
			urlMap = append(urlMap, yaml.MapItem{Key: "response_headers", Value: ref.URLMapCommon.ResponseHeaders})
		}
		if len(ref.URLMapCommon.RetryStatusCodes) > 0 {
			urlMap = append(urlMap, yaml.MapItem{Key: "retry_status_codes", Value: ref.URLMapCommon.RetryStatusCodes})
		}
		if ref.URLMapCommon.DropSrcPathPrefixParts != nil {
			urlMap = append(urlMap, yaml.MapItem{Key: "drop_src_path_prefix_parts", Value: ref.URLMapCommon.DropSrcPathPrefixParts})
		}
		if ref.URLMapCommon.LoadBalancingPolicy != nil {
			urlMap = append(urlMap, yaml.MapItem{Key: "load_balancing_policy", Value: ref.URLMapCommon.LoadBalancingPolicy})
		}
		urlMaps = append(urlMaps, urlMap)
	}
	result = append(result, yaml.MapItem{Key: "url_map", Value: urlMaps})
	return result, nil
}

// this function mutates user and fills missing fields,
// such password or username.
func genUserCfg(user *victoriametricsv1beta1.VMUser, crdURLCache map[string]string) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	r, err := genURLMaps(user.Name, user.Spec.TargetRefs, r, crdURLCache)
	if err != nil {
		return nil, fmt.Errorf("cannot generate urlMaps for user: %w", err)
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
	r = addUserConfigOptionToYaml(r, &user.Spec.UserConfigOption)
	if len(user.Spec.MetricLabels) != 0 {
		r = append(r, yaml.MapItem{
			Key:   "metric_labels",
			Value: user.Spec.MetricLabels,
		})
	}

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
		user.Spec.UserName = ptr.To(username)
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

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.UserNamespaceSelector, cr.Spec.UserSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *victoriametricsv1beta1.VMUserList) {
			for _, item := range list.Items {
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
	logger.WithContext(ctx).Info("selected VMUsers", "vmusers", strings.Join(vmUsers, ","), "namespace", cr.Namespace, "vmauth", cr.Name)

	return res, nil
}

// note, username and password must be filled by operator
// with default values if need.
func buildVMUserSecret(src *victoriametricsv1beta1.VMUser) (*v1.Secret, error) {
	s := &v1.Secret{
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
	if src.Spec.GeneratePassword && src.Spec.Password == nil {
		pwd, err := genPassword()
		if err != nil {
			return nil, fmt.Errorf("cannot generate password for user=%q: %w", src.Name, err)
		}
		src.Spec.Password = ptr.To(pwd)
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
	return s, nil
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
