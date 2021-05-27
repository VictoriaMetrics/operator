package factory

import (
	"context"
	"strings"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createOrUpdateVMAuthUsersSecret(ctx context.Context, rclient client.Client, vmauth *v1beta1.VMAuth, users []*v1beta1.VMUser) error {
	crdCache, err := FetchCRDCache(ctx, rclient, users)
	if err != nil {
		return err
	}
	newCfg, err := BuildVMAuthConfig(users, crdCache)
	if err != nil {
		return err
	}
	cfgSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{},
		Data: map[string][]byte{
			"vmauth_config.yaml": newCfg,
		},
	}
	var existSecret v1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Name: cfgSecret.Name, Namespace: cfgSecret.Namespace}, &existSecret); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, cfgSecret)
		}
	}
	v1beta1.MergeFinalizers(cfgSecret, v1beta1.FinalizerName)
	cfgSecret.Annotations = labels.Merge(cfgSecret.Annotations, existSecret.Annotations)

	return rclient.Update(ctx, cfgSecret)
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

func FetchCRDCache(ctx context.Context, rclient client.Client, users []*v1beta1.VMUser) (map[string]string, error) {
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
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlert":
				var crd v1beta1.VMAlert
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMSingle":
				var crd v1beta1.VMSingle
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url
			case "VMAlertmanager":
				var crd v1beta1.VMAlertmanager
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = url

			case "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage":
				var crd v1beta1.VMCluster
				url, err := getAsURLObject(ctx, rclient, &crd)
				if err != nil {
					return nil, err
				}
				// object not found
				// todo log
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
					// todo log error
					continue
				}
				crdCacheUrlCache[ref.CRD.AsKey()] = targetURL
			case "VMCluster/insert":
			case "VMCluster/storage":
			default:
				// todo unsupported storage.
				continue
			}
		}
	}
	return crdCacheUrlCache, nil
}

// BuildVMAuthConfig create VMAuth cfg for given Users.
func BuildVMAuthConfig(users []*v1beta1.VMUser, crdCache map[string]string) ([]byte, error) {
	var cfg yaml.MapSlice

	cfgUsers := []yaml.MapSlice{}
	for i := range users {
		user := users[i]
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
