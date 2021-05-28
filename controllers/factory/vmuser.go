package factory

import (
	"context"
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func buildVMAuthConfig(ctx context.Context, rclient client.Client, vmauth *v1beta1.VMAuth) ([]byte, error) {

	users, err := selectVMUsers(ctx, vmauth, rclient)
	if err != nil {
		return nil, err
	}
	crdCache, err := FetchCRDCache(ctx, rclient, users)
	if err != nil {
		return nil, err
	}
	// need to build secrets with user config.

	return BuildVMAuthConfig(users, crdCache)

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

func FetchCRDCache(ctx context.Context, rclient client.Client, users []v1beta1.VMUser) (map[string]string, error) {
	crdCacheUrlCache := make(map[string]string)
	for i := range users {
		user := users[i]
		for j := range user.Spec.TargetRefs {
			ref := user.Spec.TargetRefs[j]
			if ref.CRD == nil {
				continue
			}
			if _, ok := crdCacheUrlCache[ref.CRD.AsKey()]; ok {
				continue
			}
			switch name := ref.CRD.Kind; name {
			case "VMAgent":
				var crd v1beta1.VMAgent
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlert":
				var crd v1beta1.VMAlert
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMSingle":
				var crd v1beta1.VMSingle
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlertmanager":
				var crd v1beta1.VMAlertmanager
				ref.CRD.AddRefToObj(&crd)
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage":
				var crd v1beta1.VMCluster
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

// BuildVMAuthConfig create VMAuth cfg for given Users.
func BuildVMAuthConfig(users []v1beta1.VMUser, crdCache map[string]string) ([]byte, error) {
	var cfg yaml.MapSlice

	cfgUsers := []yaml.MapSlice{}
	// todo check for uniq user name.
	//uniq := make(map[string]struct{})
	for i := range users {
		user := &users[i]
		userCfg := genUserCfg(user, crdCache)
		if userCfg != nil {
			cfgUsers = append(cfgUsers, userCfg)
		}
	}

	cfg = yaml.MapSlice{
		{
			Key:   "users",
			Value: cfgUsers,
		},
	}
	return yaml.Marshal(cfg)
}

// this function assumes, that configuration was validated and has no conflicts.
// todo add ability to generate users secrets
func genUserCfg(user *v1beta1.VMUser, crdUrlCache map[string]string) yaml.MapSlice {
	r := yaml.MapSlice{}
	urlMaps := []yaml.MapSlice{}
	var username, password, token string

	for i := range user.Spec.TargetRefs {
		urlMap := yaml.MapSlice{}
		ref := user.Spec.TargetRefs[i]
		if ref.Static == nil && ref.CRD == nil {
			continue
		}
		if ref.Static != nil {
			if ref.Static.URL == "" {
				continue
			}
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "url_prefix",
				Value: ref.Static.URL,
			})
		} else {
			url := crdUrlCache[ref.CRD.AsKey()]
			if url == "" {
				continue
			}
			urlMap = append(urlMap, yaml.MapItem{
				Key:   "url_prefix",
				Value: crdUrlCache[ref.CRD.AsKey()],
			})
		}
		paths := ref.Paths
		if len(paths) == 0 {
			paths = append(paths, "/")
		}
		urlMap = append(urlMap, yaml.MapItem{
			Key:   "src_paths",
			Value: paths,
		})
		urlMaps = append(urlMaps, urlMap)
	}
	if len(urlMaps) == 0 {
		return nil
	}
	r = append(r, yaml.MapItem{Key: "url_map", Value: urlMaps})

	if user.Spec.UserName != nil {
		username = *user.Spec.UserName
	}
	if user.Spec.Password != nil {
		password = *user.Spec.Password
	}
	if user.Spec.BearerToken != nil {
		token = *user.Spec.BearerToken
	}
	needAddUserPass := token == ""
	if token != "" {
		r = append(r, yaml.MapItem{
			Key:   "bearer_token",
			Value: token,
		})
	}
	if username == "" {
		username = user.Name
	}
	if needAddUserPass {
		r = append(r, yaml.MapItem{
			Key:   "username",
			Value: username,
		})
		r = append(r, yaml.MapItem{
			Key:   "password",
			Value: password,
		})
	}

	return r
}

func selectVMUsers(ctx context.Context, cr *v1beta1.VMAuth, rclient client.Client) ([]v1beta1.VMUser, error) {

	var res []v1beta1.VMUser
	namespaces, userSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.UserNamespaceSelector, cr.Spec.UserSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMUserList{}, userSelector, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMUserList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			res = append(res, item)
		}
	}); err != nil {
		return nil, err
	}

	serviceScrapes := []string{}
	for k := range res {
		serviceScrapes = append(serviceScrapes, res[k].Name)
	}
	log.Info("selected VMUsers", "vmusers", strings.Join(serviceScrapes, ","), "namespace", cr.Namespace, "vmauth", cr.Name)

	return res, nil
}
