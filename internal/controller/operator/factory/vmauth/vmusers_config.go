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
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// allows to skip users based on external conditions
// implementation should has as less implementation details of vmusers as possible
// potentially it could be reused later for scrape objects/vmalert rules.
type skipableVMUsers struct {
	stopIter        bool
	users           []*vmv1beta1.VMUser
	brokenVMUsers   []*vmv1beta1.VMUser
	namespacedNames []string
}

// visitAll visits all users objects
func (sus *skipableVMUsers) visitAll(filter func(user *vmv1beta1.VMUser) bool) {
	var cnt int
	// filter in-place
	for _, user := range sus.users {
		if sus.stopIter {
			return
		}
		if !filter(user) {
			sus.brokenVMUsers = append(sus.brokenVMUsers, user)
			continue
		}
		sus.users[cnt] = user
		cnt++
	}
	sus.users = sus.users[:cnt]
}

func (sus *skipableVMUsers) deduplicateBy(cb func(user *vmv1beta1.VMUser) (string, time.Time)) {
	// later map[key]index could be re-place with map[key]tuple(index,timestamp)
	uniqByIndex := make(map[string]int, len(sus.users))
	var cnt int
	for idx, user := range sus.users {
		key, createdAt := cb(user)
		prevIdx, ok := uniqByIndex[key]
		if !ok {
			// fast path
			uniqByIndex[key] = idx
			sus.users[cnt] = user
			cnt++
			continue
		}
		prevUser := sus.users[prevIdx]
		if createdAt.After(prevUser.CreationTimestamp.Time) {
			prevUser, user = user, prevUser
		}
		sus.users[prevIdx] = user
		prevUser.Status.CurrentSyncError = fmt.Sprintf("user has duplicate token/password with vmuser=%s-%s", user.Namespace, user.Name)
		sus.brokenVMUsers = append(sus.brokenVMUsers, prevUser)
	}
	sus.users = sus.users[:cnt]
}

func (sus *skipableVMUsers) sort() {
	// sort for consistency.
	sort.Slice(sus.users, func(i, j int) bool {

		if sus.users[i].Name != sus.users[j].Name {
			return sus.users[i].Name < sus.users[j].Name
		}
		return sus.users[i].Namespace < sus.users[j].Namespace
	})
}

// builds vmauth config.
func buildConfig(ctx context.Context, rclient client.Client, vmauth *vmv1beta1.VMAuth, sus *skipableVMUsers, ac *build.AssetsCache) ([]byte, error) {

	// apply sort before making any changes to users
	sus.sort()

	// loads info about exist operator object kind for crdRef.
	crdCache, err := fetchCRDRefURLs(ctx, rclient, sus)
	if err != nil {
		return nil, err
	}

	toCreateSecrets, toUpdate, err := addAuthCredentialsBuildSecrets(sus, ac)
	if err != nil {
		return nil, err
	}

	// check config for dups.
	filterNonUniqUsers(sus)

	// generate yaml config for vmauth.
	cfg, err := generateVMAuthConfig(vmauth, sus, crdCache, ac)
	if err != nil {
		return nil, err
	}

	// inject generated password into secrets, that we want to create.
	if err := createVMUserSecrets(ctx, rclient, toCreateSecrets); err != nil {
		return nil, err
	}
	// update secrets.
	for i := range toUpdate {
		secret := toUpdate[i]
		logger.WithContext(ctx).Info(fmt.Sprintf("updating vmuser secret %s configuration", secret.Name))

		if err := rclient.Update(ctx, secret); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func createVMUserSecrets(ctx context.Context, rclient client.Client, secrets []*corev1.Secret) error {
	for i := range secrets {
		secret := secrets[i]
		if err := rclient.Create(ctx, secret); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// duplicates logic from vmauth auth_config
// parseAuthConfigUsers
func filterNonUniqUsers(sus *skipableVMUsers) {
	sus.deduplicateBy(func(user *vmv1beta1.VMUser) (string, time.Time) {
		var at string
		if user.Spec.UserName != nil {
			at = "basicAuth:" + *user.Spec.UserName
		}
		if user.Spec.Password != nil {
			at += ":" + *user.Spec.Password
		}
		if user.Spec.BearerToken != nil {
			at = "bearerToken:" + *user.Spec.BearerToken
		}
		return at, user.CreationTimestamp.Time
	})
}

type objectWithURL interface {
	client.Object
	AsURL() string
}

func getAsURLObject(ctx context.Context, rclient client.Client, objT objectWithURL) (string, error) {
	obj := objT.(client.Object)
	// dirty hack to restore original type of vmcluster
	// since cluster type erased by wrapping it into clusterWithURL
	// we must restore it original type by unwrapping type
	uw, ok := objT.(unwrapObject)
	if ok {
		obj = uw.origin()
	}
	if err := rclient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj); err != nil {
		if build.IsNotFound(err) {
			return "", fmt.Errorf("cannot find object by the given ref,namespace=%q,name=%q: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		return "", fmt.Errorf("cannot get object by given ref namespace=%q,name=%q: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return objT.AsURL(), nil
}

func addAuthCredentialsBuildSecrets(sus *skipableVMUsers, ac *build.AssetsCache) (needToCreateSecrets []*corev1.Secret, needToUpdateSecrets []*corev1.Secret, resultErr error) {
	sus.visitAll(func(user *vmv1beta1.VMUser) bool {
		switch {
		case user.Spec.PasswordRef != nil:
			secret, err := ac.LoadKeyFromSecret(user.Namespace, user.Spec.PasswordRef)
			if err != nil {
				if !build.IsNotFound(err) {
					resultErr = fmt.Errorf("cannot get cred from secret=%w", err)
					sus.stopIter = true
					return true
				}
				user.Status.CurrentSyncError = fmt.Sprintf("cannot get cred from secret for passwordRef: %q", err)
				return false
			}
			user.Spec.Password = ptr.To(secret)
		case user.Spec.TokenRef != nil:
			secret, err := ac.LoadKeyFromSecret(user.Namespace, user.Spec.TokenRef)
			if err != nil {
				user.Status.CurrentSyncError = fmt.Sprintf("cannot get cred from secret for tokenRef: %q", err)
				return false
			}
			user.Spec.BearerToken = ptr.To(secret)
		}

		if !user.Spec.DisableSecretCreation {
			secret, err := ac.LoadSecret(user.Namespace, user.SecretName())
			if err != nil {
				if !build.IsNotFound(err) {
					resultErr = fmt.Errorf("cannot get secret from API=%w", err)
					sus.stopIter = true
					return true
				}
				userSecret, err := buildVMUserSecret(user)
				if err != nil {
					user.Status.CurrentSyncError = fmt.Sprintf("cannot build user secret with password: %q", err)
					return false
				}
				needToCreateSecrets = append(needToCreateSecrets, userSecret)

			} else if injectAuthSettings(secret, user) {
				// secret exists, check it's state
				needToUpdateSecrets = append(needToUpdateSecrets, secret)
			}
		}
		if err := injectBackendAuthHeader(user, ac); err != nil {
			if !build.IsNotFound(err) {
				resultErr = fmt.Errorf("cannot inject backend auth header=%w", err)
				sus.stopIter = true
				return true
			}
			user.Status.CurrentSyncError = fmt.Sprintf("cannot inject auth backend header : %q", err)
			return false
		}

		return true
	})

	return
}

func injectBackendAuthHeader(user *vmv1beta1.VMUser, ac *build.AssetsCache) error {
	for j := range user.Spec.TargetRefs {
		ref := &user.Spec.TargetRefs[j]
		if ref.TargetRefBasicAuth != nil {
			creds, err := ac.BuildBasicAuthCreds(user.Namespace, &vmv1beta1.BasicAuth{
				Username: ref.TargetRefBasicAuth.Username,
				Password: ref.TargetRefBasicAuth.Password,
			})
			if err != nil {
				return fmt.Errorf("could not load basicAuth config: %w", err)
			}
			token := creds.Username + ":" + creds.Password
			token64 := base64.StdEncoding.EncodeToString([]byte(token))
			Header := "Authorization: Basic " + token64
			ref.RequestHeaders = append(ref.RequestHeaders, Header)
		}
	}
	return nil
}

func injectAuthSettings(secret *corev1.Secret, vmuser *vmv1beta1.VMUser) bool {
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

var crdNameToObject = map[string]objectWithURL{
	"VMAgent":  &vmv1beta1.VMAgent{},
	"VMAlert":  &vmv1beta1.VMAlert{},
	"VMSingle": &vmv1beta1.VMSingle{},
	"VLogs":    &vmv1beta1.VLogs{},
	// keep both variants for backward-compatibility
	"VMAlertmanager":      &vmv1beta1.VMAlertmanager{},
	"VMAlertManager":      &vmv1beta1.VMAlertmanager{},
	"VMCluster/vmselect":  newClusterWithURL("vmselect"),
	"VMCluster/vminsert":  newClusterWithURL("vminsert"),
	"VMCluster/vmstorage": newClusterWithURL("vmstorage"),
	"VLSingle":            &vmv1.VLSingle{},
	"VLCluster/vlselect":  newClusterWithURL("vlselect"),
	"VLCluster/vlinsert":  newClusterWithURL("vlinsert"),
	"VLCluster/vlstorage": newClusterWithURL("vlstorage"),
	"VLAgent":             &vmv1.VLAgent{},
}

// helper interface to restore VMCluster type
type unwrapObject interface {
	origin() client.Object
}

var clusterComponentToURL = map[string]func(obj client.Object) string{
	"vminsert": func(obj client.Object) string {
		return obj.(*vmv1beta1.VMCluster).VMInsertURL()
	},
	"vmselect": func(obj client.Object) string {
		return obj.(*vmv1beta1.VMCluster).VMSelectURL()
	},
	"vmstorage": func(obj client.Object) string {
		return obj.(*vmv1beta1.VMCluster).VMStorageURL()
	},
	"vlinsert": func(obj client.Object) string {
		return obj.(*vmv1.VLCluster).SelectURL()
	},
	"vlselect": func(obj client.Object) string {
		return obj.(*vmv1.VLCluster).InsertURL()
	},
	"vlstorage": func(obj client.Object) string {
		return obj.(*vmv1.VLCluster).StorageURL()
	},
}

type clusterWithURL struct {
	client.Object
	vmc       *vmv1beta1.VMCluster
	component string
}

func newClusterWithURL(component string) *clusterWithURL {
	vmc := &vmv1beta1.VMCluster{}
	return &clusterWithURL{vmc, vmc, component}
}

func (c *clusterWithURL) origin() client.Object {
	return c.vmc
}

// AsURL implements AsURL interface
func (c *clusterWithURL) AsURL() string {
	builder, ok := clusterComponentToURL[c.component]
	if !ok {
		panic(fmt.Sprintf("BUG: not expected component=%q for clusterWithURL object", c.component))
	}
	return builder(c.Object)
}

// fetchCRDRefURLs performs a fetch for CRD objects for vmauth users and returns an url by crd ref key name
func fetchCRDRefURLs(ctx context.Context, rclient client.Client, sus *skipableVMUsers) (map[string]string, error) {
	crdCacheURLCache := make(map[string]string)
	var resultErr error
	sus.visitAll(func(user *vmv1beta1.VMUser) bool {
		for j := range user.Spec.TargetRefs {
			ref := user.Spec.TargetRefs[j]
			if ref.CRD == nil {
				continue
			}
			if _, ok := crdCacheURLCache[ref.CRD.AsKey()]; ok {
				continue
			}
			crdObj, ok := crdNameToObject[ref.CRD.Kind]
			if !ok {
				user.Status.CurrentSyncError = fmt.Sprintf("unsupported kind for ref: %q at idx=%d", ref.CRD.Kind, j)
				return false
			}
			ref.CRD.AddRefToObj(crdObj.(client.Object))
			url, err := getAsURLObject(ctx, rclient, crdObj)
			if err != nil {
				if !build.IsNotFound(err) {
					resultErr = fmt.Errorf("cannot get object as url: %w", err)
					sus.stopIter = true
					return true
				}
				user.Status.CurrentSyncError = fmt.Sprintf("cannot fined CRD link for kind=%q at ref idx=%d: %q", ref.CRD.Kind, j, err)
				return false
			}
			crdCacheURLCache[ref.CRD.AsKey()] = url
		}
		return true
	})
	return crdCacheURLCache, resultErr
}

// generateVMAuthConfig create VMAuth cfg for given Users.
func generateVMAuthConfig(cr *vmv1beta1.VMAuth, sus *skipableVMUsers, crdCache map[string]string, ac *build.AssetsCache) ([]byte, error) {
	var cfg yaml.MapSlice

	var cfgUsers []yaml.MapSlice

	sus.visitAll(func(user *vmv1beta1.VMUser) bool {
		userCfg, err := genUserCfg(user, crdCache, cr, ac)
		if err != nil {
			user.Status.CurrentSyncError = err.Error()
			return false
		}
		cfgUsers = append(cfgUsers, userCfg)
		return true
	})

	if len(cfgUsers) > 0 {
		cfg = yaml.MapSlice{
			{
				Key:   "users",
				Value: cfgUsers,
			},
		}
	}

	unAuthorizedAccessValue, err := buildUnauthorizedConfig(cr, ac)
	if err != nil {
		return nil, fmt.Errorf("cannot build unauthorized_user config section: %w", err)
	}

	if len(unAuthorizedAccessValue) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "unauthorized_user", Value: unAuthorizedAccessValue})
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize configuration to yaml: %w", err)
	}
	return data, nil
}

func appendIfNotEmpty(src []string, key string, origin yaml.MapSlice) yaml.MapSlice {
	if len(src) > 0 {
		return append(origin, yaml.MapItem{
			Key:   key,
			Value: src,
		})
	}
	return origin
}

func buildUnauthorizedConfig(cr *vmv1beta1.VMAuth, ac *build.AssetsCache) ([]yaml.MapItem, error) {
	var result []yaml.MapItem

	switch {
	case cr.Spec.UnauthorizedUserAccessSpec != nil:
		uua := cr.Spec.UnauthorizedUserAccessSpec
		if err := uua.Validate(); err != nil {
			return nil, fmt.Errorf("incorrect spec.UnauthorizedUserAccess syntax: %w", err)
		}
		var urlMapYAML []yaml.MapSlice
		for _, uc := range uua.URLMap {
			urlMap := appendIfNotEmpty(uc.SrcPaths, "src_paths", yaml.MapSlice{})
			urlMap = appendIfNotEmpty(uc.SrcHosts, "src_hosts", urlMap)
			urlMap = appendIfNotEmpty(uc.URLPrefix, "url_prefix", urlMap)
			urlMap = addURLMapCommonToYaml(urlMap, uc.URLMapCommon, false)
			urlMapYAML = append(urlMapYAML, urlMap)
		}
		if len(urlMapYAML) > 0 {
			result = append(result, yaml.MapItem{Key: "url_map", Value: urlMapYAML})
		}
		if len(uua.URLPrefix) > 0 {
			result = append(result, yaml.MapItem{Key: "url_prefix", Value: uua.URLPrefix})
		}
		if len(uua.MetricLabels) > 0 {
			result = append(result, yaml.MapItem{
				Key:   "metric_labels",
				Value: uua.MetricLabels,
			})
		}
		var err error
		result, err = addUserConfigOptionToYaml(result, uua.VMUserConfigOptions, cr, ac)
		if err != nil {
			return nil, err
		}

	case len(cr.Spec.UnauthorizedAccessConfig) > 0: //nolint:staticcheck
		// Deprecated and will be removed at v1.0
		var urlMapYAML []yaml.MapSlice
		for _, uc := range cr.Spec.UnauthorizedAccessConfig { //nolint:staticcheck
			urlMap := appendIfNotEmpty(uc.SrcPaths, "src_paths", yaml.MapSlice{})
			urlMap = appendIfNotEmpty(uc.SrcHosts, "src_hosts", urlMap)
			urlMap = appendIfNotEmpty(uc.URLPrefix, "url_prefix", urlMap)

			urlMap = addURLMapCommonToYaml(urlMap, uc.URLMapCommon, false)
			urlMapYAML = append(urlMapYAML, urlMap)
		}
		result = append(result, yaml.MapItem{Key: "url_map", Value: urlMapYAML})

		var err error
		result, err = addUserConfigOptionToYaml(result, cr.Spec.VMUserConfigOptions, cr, ac)
		if err != nil {
			return nil, err
		}

	default:
		return nil, nil
	}
	return result, nil
}

func addURLMapCommonToYaml(dst yaml.MapSlice, opt vmv1beta1.URLMapCommon, isDefaultRoute bool) yaml.MapSlice {
	if !isDefaultRoute {
		dst = appendIfNotEmpty(opt.SrcQueryArgs, "src_query_args", dst)
		dst = appendIfNotEmpty(opt.SrcHeaders, "src_headers", dst)
	}
	dst = appendIfNotEmpty(opt.RequestHeaders, "headers", dst)
	dst = appendIfNotEmpty(opt.ResponseHeaders, "response_headers", dst)
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

func addUserConfigOptionToYaml(dst yaml.MapSlice, opt vmv1beta1.VMUserConfigOptions, cr *vmv1beta1.VMAuth, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if len(opt.DefaultURLs) > 0 {
		dst = append(dst, yaml.MapItem{Key: "default_url", Value: opt.DefaultURLs})
	}
	cfg, err := ac.TLSToYAML(cr.Namespace, "tls_", opt.TLSConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build tls config for vmauth %s under %s, err: %v", cr.Name, cr.Namespace, err)
	}
	if len(cfg) > 0 {
		dst = append(dst, cfg...)
	}
	dst = addIPFiltersToYaml(dst, opt.IPFilters)
	dst = appendIfNotEmpty(opt.Headers, "headers", dst)
	dst = appendIfNotEmpty(opt.ResponseHeaders, "response_headers", dst)
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
	if opt.DumpRequestOnErrors != nil {
		dst = append(dst, yaml.MapItem{
			Key:   "dump_request_on_errors",
			Value: *opt.DumpRequestOnErrors,
		})
	}
	return dst, nil
}

// AddToYaml conditionally adds ip filters to dst yaml
func addIPFiltersToYaml(dst yaml.MapSlice, ipf vmv1beta1.VMUserIPFilters) yaml.MapSlice {
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
func genURLMaps(userName string, refs []vmv1beta1.TargetRef, result yaml.MapSlice, crdURLCache map[string]string) (yaml.MapSlice, error) {
	var urlMaps []yaml.MapSlice
	handleRef := func(ref vmv1beta1.TargetRef) ([]string, error) {
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
		if ref.DiscoverBackendIPs != nil {
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "discover_backend_ips",
				Value: *ref.DiscoverBackendIPs,
			},
			)
		}
		urlMap = appendIfNotEmpty(ref.SrcHeaders, "src_headers", urlMap)
		urlMap = appendIfNotEmpty(ref.SrcQueryArgs, "src_query_args", urlMap)
		if len(ref.RequestHeaders) > 0 {
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "headers",
				Value: ref.RequestHeaders,
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
	if len(urlMaps) == 0 {
		return nil, fmt.Errorf("user must has at least 1 url target")
	}
	result = append(result, yaml.MapItem{Key: "url_map", Value: urlMaps})
	return result, nil
}

// this function mutates user and fills missing fields,
// such password or username.
func genUserCfg(user *vmv1beta1.VMUser, crdURLCache map[string]string, cr *vmv1beta1.VMAuth, ac *build.AssetsCache) (yaml.MapSlice, error) {
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
	r, err = addUserConfigOptionToYaml(r, user.Spec.VMUserConfigOptions, cr, ac)
	if err != nil {
		return nil, err
	}
	if len(user.Spec.MetricLabels) > 0 {
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
func selectVMUsers(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) (*skipableVMUsers, error) {
	var res []*vmv1beta1.VMUser
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.UserSelector,
		NamespaceSelector: cr.Spec.UserNamespaceSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMUserList) {
		for _, item := range list.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			item.Status.ObservedGeneration = item.GetGeneration()
			res = append(res, item.DeepCopy())
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}
	return &skipableVMUsers{users: res, namespacedNames: namespacedNames}, nil
}

// note, username and password must be filled by operator
// with default values if need.
func buildVMUserSecret(src *vmv1beta1.VMUser) (*corev1.Secret, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            src.SecretName(),
			Namespace:       src.Namespace,
			Labels:          src.AllLabels(),
			Annotations:     src.AnnotationsFiltered(),
			OwnerReferences: src.AsOwner(),
			Finalizers: []string{
				vmv1beta1.FinalizerName,
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
