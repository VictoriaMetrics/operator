/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	v12 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMUserReconciler reconciles a VMUser object
type VMUserReconciler struct {
	client.Client
	BaseConf     *config.BaseOperatorConf
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Scheme implements interface.
func (r *VMUserReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// secretRefCache allows to track secrets used by vmuser passwordRef
type vmUsersSecretRefCache struct {
	mu sync.Mutex

	secretNames map[string][]types.NamespacedName
}

var globalSecretRefCache vmUsersSecretRefCache

func init() {
	globalSecretRefCache.secretNames = make(map[string][]types.NamespacedName)
}

func (v *vmUsersSecretRefCache) addRefByUser(user *operatorv1beta1.VMUser) {
	if user.Spec.PasswordRef == nil {
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	nn := types.NamespacedName{Name: user.Name, Namespace: user.Namespace}
	key := fmt.Sprintf("%s/%s", user.Namespace, user.Spec.PasswordRef.Name)
	if existRefs, ok := v.secretNames[key]; ok {

		// usually its fast, no need for map.
		for i := range existRefs {
			ref := existRefs[i]
			if ref == nn {
				return
			}
		}
		existRefs = append(existRefs, nn)
		v.secretNames[key] = existRefs
		return
	}
	v.secretNames[key] = []types.NamespacedName{nn}
}

// removes given user from secret cache.
func (v *vmUsersSecretRefCache) removeUserRef(user types.NamespacedName) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for key, userNNs := range v.secretNames {
		// trim in-place.
		var i int
		for _, nn := range userNNs {
			if nn != user {
				userNNs[i] = nn
				i++
			}
		}
		if i != len(userNNs) {
			userNNs = userNNs[:i]
			v.secretNames[key] = userNNs
		}
		if len(userNNs) == 0 {
			delete(v.secretNames, key)
		}
	}
}

func (v *vmUsersSecretRefCache) getUserBySecret(namespacedSecret string) []types.NamespacedName {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.secretNames[namespacedSecret]
}

// StartWatchForVMUserSecretRefs its needed to dynamically watch for secrets updates.
func StartWatchForVMUserSecretRefs(ctx context.Context, rclient client.Client, cfg *rest.Config) error {

	c, err := v12.NewForConfig(cfg)
	if err != nil {
		return err
	}
	secretSelector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/name":      "vmuser",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}).String()
	inf := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.Secrets(config.MustGetWatchNamespace()).List(ctx, metav1.ListOptions{LabelSelector: secretSelector})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.Secrets(config.MustGetWatchNamespace()).Watch(ctx, metav1.ListOptions{LabelSelector: secretSelector})
			},
		},
		&v1.Secret{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(prevObj, newObj interface{}) {
			// trigger related VMUser update by changing some annotation.
			changedSecret := newObj.(*v1.Secret)
			prevSecret := prevObj.(*v1.Secret)
			if reflect.DeepEqual(prevSecret.Data, changedSecret.Data) {
				return
			}
			key := fmt.Sprintf("%s/%s", changedSecret.Namespace, changedSecret.Name)
			var vmuser operatorv1beta1.VMUser
			for _, nn := range globalSecretRefCache.getUserBySecret(key) {
				if err := rclient.Get(ctx, nn, &vmuser); err != nil {
					if errors.IsNotFound(err) {
						continue
					}
					log.Error(err, "cannot get vmuser for secret")
					continue
				}
				// handle case, when password ref was removed from given user.
				if vmuser.Spec.PasswordRef == nil {
					globalSecretRefCache.removeUserRef(types.NamespacedName{Name: vmuser.Name, Namespace: vmuser.Namespace})
					continue
				}
				vmuser.ObjectMeta.Annotations["last-password-ref-secret-update"] = time.Now().Format(time.RFC3339)
				// safe to ignore error.
				_ = rclient.Update(ctx, &vmuser)
			}
		},
	})
	go inf.Run(ctx.Done())
	return nil
}

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers/status,verbs=get;update;patch
func (r *VMUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := r.Log.WithValues("vmuser", req.NamespacedName)

	var instance operatorv1beta1.VMUser

	err := r.Get(ctx, req.NamespacedName, &instance)
	if err != nil {
		return handleGetError(req, "vmuser", err)
	}

	// lock vmauth sync.
	vmAuthSyncMU.Lock()
	defer vmAuthSyncMU.Unlock()

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.AddFinalizer(ctx, r.Client, &instance); err != nil {
			return ctrl.Result{}, err
		}
		RegisterObject(instance.Name, instance.Namespace, "vmuser")
	}

	globalSecretRefCache.addRefByUser(&instance)

	var vmauthes operatorv1beta1.VMAuthList
	if err := r.List(ctx, &vmauthes, config.MustGetNamespaceListOptions()); err != nil {
		l.Error(err, "cannot list VMAuth at cluster wide.")
		return ctrl.Result{}, err
	}
	for _, vmauth := range vmauthes.Items {
		if !vmauth.DeletionTimestamp.IsZero() || vmauth.Spec.ParsingError != "" {
			continue
		}
		// reconcile users for given vmauth.
		currentVMAuth := &vmauth
		l = l.WithValues("vmauth", vmauth.Name)
		match, err := isSelectorsMatches(&instance, currentVMAuth, currentVMAuth.Spec.UserSelector)
		if err != nil {
			l.Error(err, "cannot match vmauth and VMUser")
			continue
		}
		// fast path
		if !match {
			continue
		}
		l.Info("reconciling vmuser for vmauth")
		if err := factory.CreateOrUpdateVMAuth(ctx, currentVMAuth, r, r.BaseConf); err != nil {
			l.Error(err, "cannot create or update vmauth deploy")
			return ctrl.Result{}, err
		}
	}
	if !instance.DeletionTimestamp.IsZero() {
		// need to remove finalizer and delete related resources.
		if err := finalize.OnVMUserDelete(ctx, r, &instance); err != nil {
			l.Error(err, "cannot remove finalizer")
			return ctrl.Result{}, err
		}
		globalSecretRefCache.removeUserRef(req.NamespacedName)
		DeregisterObject(instance.Name, instance.Namespace, "vmuser")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager inits object
func (r *VMUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMUser{}).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		WithOptions(defaultOptions).
		Complete(r)
}
