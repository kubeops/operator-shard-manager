/*
Copyright AppsCode Inc. and Contributors.

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

package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	shardapi "kubeops.dev/operator-shard-manager/api/v1alpha1"

	"gomodules.xyz/consistent"
	apps "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	kmapi "kmodules.xyz/client-go/api/v1"
	core_util "kmodules.xyz/client-go/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ShardConfigurationReconciler reconciles a ShardConfiguration object
type ShardConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	ctrl   controller.Controller
	cache  cache.Cache
	d      discovery.DiscoveryInterface
	mapper meta.RESTMapper

	resMu  sync.Mutex
	resGKs map[schema.GroupKind]struct{}
}

func NewShardConfigurationReconciler(mgr manager.Manager, d discovery.DiscoveryInterface, mapper meta.RESTMapper) *ShardConfigurationReconciler {
	return &ShardConfigurationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		cache:  mgr.GetCache(),
		d:      d,
		mapper: mapper,
		resGKs: make(map[schema.GroupKind]struct{}),
	}
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ShardConfiguration object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *ShardConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var cfg shardapi.ShardConfiguration
	if err := r.Get(ctx, req.NamespacedName, &cfg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	klog.V(5).Infof("Reconciling ShardConfiguration %s", cfg.Name)
	ctrlMap := make(map[kmapi.TypedObjectReference][]string)
	for _, ref := range cfg.Status.Controllers {
		ctrlMap[ref.TypedObjectReference] = ref.Pods
	}
	allocs := make([]shardapi.ControllerAllocation, 0, len(cfg.Spec.Controllers))

	shardCount := -1
	for _, ref := range cfg.Spec.Controllers {
		podLists, err := ListPods(ctx, r.Client, ref)
		if apierrors.IsNotFound(err) {
			continue
		} else if err != nil {
			return ctrl.Result{}, err
		}

		if shardCount == -1 {
			shardCount = len(podLists)
		} else if shardCount != len(podLists) {
			klog.Infof("expected %d shards, got %d for controller %s/%s %s/%s", shardCount, len(podLists), ref.APIGroup, ref.Kind, ref.Namespace, ref.Name)
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}

		if existing, ok := ctrlMap[ref]; !ok {
			allocs = append(allocs, shardapi.ControllerAllocation{
				TypedObjectReference: ref,
				Pods:                 podLists,
			})
		} else {
			allocs = append(allocs, shardapi.ControllerAllocation{
				TypedObjectReference: ref,
				Pods:                 getUpdatedPodLists(existing, podLists),
			})
		}
	}
	if cfg.DeletionTimestamp != nil {
		if shardCount <= 0 {
			_, err := controllerutil.CreateOrPatch(ctx, r.Client, &cfg, func() error {
				cfg.ObjectMeta = core_util.RemoveFinalizer(cfg.ObjectMeta, shardapi.SchemeGroupVersion.Group)
				return nil
			})
			return ctrl.Result{}, err
		} else {
			// shardCount is greater than 0 means, this shardConfig is getting used by some controllers. We will not let this get deleted in that case.
			klog.Infof("Config %v is in use by %v. Can't delete it.", cfg.Name, cfg.Spec.Controllers)
		}
	}
	opresult, err := controllerutil.CreateOrPatch(ctx, r.Client, &cfg, func() error {
		cfg.ObjectMeta = core_util.AddFinalizer(cfg.ObjectMeta, shardapi.SchemeGroupVersion.Group)
		cfg.Status.Controllers = allocs
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if opresult != controllerutil.OperationResultNone {
		log.Info(string(opresult))
	}
	if shardCount <= 0 {
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}
	members := make([]consistent.Member, 0, shardCount)
	for i := 0; i < shardCount; i++ {
		members = append(members, Member{ID: i})
	}
	cc := newConsistentConfig(members, shardCount)
	for _, resource := range cfg.Spec.Resources {
		if resource.Kind != "" {
			mapping, err := r.mapper.RESTMapping(schema.GroupKind{
				Group: resource.APIGroup,
				Kind:  resource.Kind,
			})
			if err != nil {
				return ctrl.Result{}, err
			}

			gvk := mapping.GroupVersionKind
			if err := r.RegisterResourceWatcher(gvk); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.UpdateShardLabel(ctx, cc, gvk, &cfg); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			resourceLists, err := r.d.ServerPreferredResources()
			if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
				return ctrl.Result{}, err
			}
			for _, resourceList := range resourceLists {
				if gv, err := schema.ParseGroupVersion(resourceList.GroupVersion); err == nil && gv.Group == resource.APIGroup {
					for _, apiResource := range resourceList.APIResources {
						if isReadable(apiResource.Verbs) {
							gvk := gv.WithKind(apiResource.Kind)
							if err := r.RegisterResourceWatcher(gvk); err != nil {
								return ctrl.Result{}, err
							}
							if err := r.UpdateShardLabel(ctx, cc, gvk, &cfg); err != nil {
								return ctrl.Result{}, err
							}
						}
					}
				}
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ShardConfigurationReconciler) UpdateShardLabel(ctx context.Context, cc *consistent.Consistent, gvk schema.GroupVersionKind, cfg *shardapi.ShardConfiguration) error {
	log := log.FromContext(ctx)
	shardKey := fmt.Sprintf("shard.%s/%s", shardapi.SchemeGroupVersion.Group, cfg.Name)
	var list metav1.PartialObjectMetadataList
	list.SetGroupVersionKind(gvk)
	err := r.List(ctx, &list)
	if err != nil {
		return err
	}
	for _, obj := range list.Items {
		m := cc.LocateKey([]byte(fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())))

		if obj.Labels[shardKey] != m.String() {
			opr, err := controllerutil.CreateOrPatch(ctx, r.Client, &obj, func() error {
				if obj.Labels == nil {
					obj.Labels = map[string]string{}
				}
				obj.Labels[shardKey] = m.String()
				return nil
			})
			if err != nil {
				log.Error(err, fmt.Sprintf("failed to update labels for %s/%s %s/%s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName()))
			} else {
				log.Info(fmt.Sprintf("%s/%s %s/%s %s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), opr))
			}
		}
	}
	return nil
}

func (r *ShardConfigurationReconciler) RegisterResourceWatcher(gvk schema.GroupVersionKind) error {
	r.resMu.Lock()
	defer r.resMu.Unlock()
	if _, ok := r.resGKs[gvk.GroupKind()]; ok {
		return nil
	}

	var obj metav1.PartialObjectMetadata
	obj.SetGroupVersionKind(gvk)
	err := r.ctrl.Watch(source.Kind[*metav1.PartialObjectMetadata](r.cache, &obj, handler.TypedEnqueueRequestsFromMapFunc[*metav1.PartialObjectMetadata](func(ctx context.Context, md *metav1.PartialObjectMetadata) []reconcile.Request {
		var list shardapi.ShardConfigurationList
		err := r.List(context.TODO(), &list)
		if err != nil {
			return nil
		}

		var result []reconcile.Request
		for _, item := range list.Items {
			for _, resource := range item.Spec.Resources {
				if resource.APIGroup == gvk.Group && (resource.Kind == "" || resource.Kind == gvk.Kind) {
					result = append(result, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: item.Name,
						},
					})
				}
			}
		}
		return result
	})))
	if err != nil {
		return err
	}
	r.resGKs[gvk.GroupKind()] = struct{}{}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	appHandler := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list shardapi.ShardConfigurationList
		err := r.List(context.TODO(), &list)
		if err != nil {
			return nil
		}
		gvk, err := apiutil.GVKForObject(obj, r.Scheme)
		if err != nil {
			return nil
		}

		var result []reconcile.Request
		for _, item := range list.Items {
			for _, resource := range item.Spec.Controllers {
				if resource.APIGroup == gvk.Group && resource.Kind == gvk.Kind {
					result = append(result, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: item.Name,
						},
					})
				}
			}
		}
		return result
	})

	var err error
	r.ctrl, err = ctrl.NewControllerManagedBy(mgr).
		For(&shardapi.ShardConfiguration{}).
		Watches(&apps.Deployment{}, appHandler).
		Watches(&apps.DaemonSet{}, appHandler).
		Watches(&apps.StatefulSet{}, appHandler).
		Named("shardconfiguration").
		Build(r)
	return err
}

func ListPods(ctx context.Context, kc client.Client, ref kmapi.TypedObjectReference) ([]string, error) {
	// TODO: Ignore terminating pods
	if ref.APIGroup != "apps" {
		return nil, errors.New("controller must be one of Deployment/StatefulSet/Daemonset")
	}
	switch ref.Kind {
	case "Deployment":
		var obj apps.Deployment
		err := kc.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, &obj)
		if err != nil {
			return nil, err
		}
		sel, err := metav1.LabelSelectorAsSelector(obj.Spec.Selector)
		if err != nil {
			return nil, err
		}

		var list metav1.PartialObjectMetadataList
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Kind:    "Pod",
			Version: "v1",
		})
		err = kc.List(ctx, &list, client.MatchingLabelsSelector{Selector: sel})
		if err != nil {
			return nil, err
		}
		pods := make([]string, 0, len(list.Items))
		for _, pod := range list.Items {
			rsRef := metav1.GetControllerOfNoCopy(&pod)
			if rsRef == nil || rsRef.Kind != "ReplicaSet" {
				continue
			}
			var rs apps.ReplicaSet
			err = kc.Get(ctx, client.ObjectKey{Name: rsRef.Name, Namespace: ref.Namespace}, &rs)
			if err != nil {
				return nil, err
			}
			if metav1.IsControlledBy(&pod, &rs) && pod.DeletionTimestamp == nil {
				pods = append(pods, pod.Name)
			}
		}
		sort.Strings(pods)
		return pods, nil
	case "StatefulSet":
		var obj apps.StatefulSet
		err := kc.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, &obj)
		if err != nil {
			return nil, err
		}
		sel, err := metav1.LabelSelectorAsSelector(obj.Spec.Selector)
		if err != nil {
			return nil, err
		}
		var list metav1.PartialObjectMetadataList
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Kind:    "Pod",
			Version: "v1",
		})
		err = kc.List(ctx, &list, client.MatchingLabelsSelector{Selector: sel})
		if err != nil {
			return nil, err
		}
		pods := make([]string, 0, len(list.Items))
		for _, pod := range list.Items {
			if metav1.IsControlledBy(&pod, &obj) {
				pods = append(pods, pod.Name)
			}
		}
		sort.Slice(pods, func(i, j int) bool {
			idx_i := strings.LastIndexByte(pods[i], '-')
			idx_j := strings.LastIndexByte(pods[j], '-')
			if idx_i == -1 || idx_j == -1 {
				return pods[i] < pods[j]
			}
			oi, err_i := strconv.Atoi(pods[i][idx_i+1:])
			oj, err_j := strconv.Atoi(pods[j][idx_j+1:])
			if err_i != nil || err_j != nil {
				return pods[i] < pods[j]
			}
			return oi < oj
		})
		return pods, nil
	case "DaemonSet":
		var obj apps.DaemonSet
		err := kc.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, &obj)
		if err != nil {
			return nil, err
		}
		sel, err := metav1.LabelSelectorAsSelector(obj.Spec.Selector)
		if err != nil {
			return nil, err
		}
		var list metav1.PartialObjectMetadataList
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Kind:    "Pod",
			Version: "v1",
		})
		err = kc.List(ctx, &list, client.MatchingLabelsSelector{Selector: sel})
		if err != nil {
			return nil, err
		}
		pods := make([]string, 0, len(list.Items))
		for _, pod := range list.Items {
			if metav1.IsControlledBy(&pod, &obj) && pod.DeletionTimestamp == nil {
				pods = append(pods, pod.Name)
			}
		}
		sort.Strings(pods)
		return pods, nil
	default:
		return nil, errors.New("controller must be one of Deployment/StatefulSet/DaemonSet")
	}
}
