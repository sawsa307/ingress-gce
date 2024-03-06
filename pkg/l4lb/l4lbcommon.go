/*
Copyright 2021 The Kubernetes Authors.

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

package l4lb

import (
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// computeNewAnnotationsIfNeeded checks if new annotations should be added to service.
// If needed creates new service meta object.
// This function is used by External and Internal L4 LB controllers.
func computeNewAnnotationsIfNeeded(svc *v1.Service, newAnnotations map[string]string, keysToRemove []string) *metav1.ObjectMeta {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = mergeAnnotations(newObjectMeta.Annotations, newAnnotations, keysToRemove)
	if reflect.DeepEqual(svc.Annotations, newObjectMeta.Annotations) {
		return nil
	}
	return newObjectMeta
}

// mergeAnnotations merges the new set of l4lb resource annotations with the preexisting service annotations.
// Existing L4 resource annotation values will be replaced with the values in the new map.
// This function is used by External and Internal L4 LB controllers.
func mergeAnnotations(existing, lbAnnotations map[string]string, keysToRemove []string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	} else {
		// Delete existing annotations.
		for _, key := range keysToRemove {
			delete(existing, key)
		}
	}
	// merge existing annotations with the newly added annotations
	for key, val := range lbAnnotations {
		existing[key] = val
	}
	return existing
}

// updateL4ResourcesAnnotations checks if new annotations should be added to service and patch service metadata if needed.
func updateL4ResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, logger klog.Logger) error {
	logger.V(3).Info("Updating annotations of service", "serviceKey", klog.KRef(svc.Namespace, svc.Name))
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4ResourceAnnotationKeys)
	if newObjectMeta == nil {
		logger.V(3).Info("Service annotations not changed, skipping patch for service", "serviceKey", klog.KRef(svc.Namespace, svc.Name))
		return nil
	}
	logger.V(3).Info("Patching annotations of service", "serviceKey", klog.KRef(svc.Namespace, svc.Name))
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateL4DualStackResourcesAnnotations checks if new annotations should be added to dual-stack service and patch service metadata if needed.
func updateL4DualStackResourcesAnnotations(ctx *context.ControllerContext, svc *v1.Service, newL4LBAnnotations map[string]string, logger klog.Logger) error {
	newObjectMeta := computeNewAnnotationsIfNeeded(svc, newL4LBAnnotations, loadbalancers.L4DualStackResourceAnnotationKeys)
	if newObjectMeta == nil {
		return nil
	}
	logger.V(3).Info("Patching annotations of service", "serviceKey", klog.KRef(svc.Namespace, svc.Name))
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

func deleteAnnotation(ctx *context.ControllerContext, svc *v1.Service, annotationKey string, logger klog.Logger) error {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	if _, ok := newObjectMeta.Annotations[annotationKey]; !ok {
		return nil
	}

	logger.V(3).Info("Removing annotation from service", "annotationKey", annotationKey, "serviceKey", klog.KRef(svc.Namespace, svc.Name))
	delete(newObjectMeta.Annotations, annotationKey)
	return patch.PatchServiceObjectMetadata(ctx.KubeClient.CoreV1(), svc, *newObjectMeta)
}

// updateServiceStatus this faction checks if LoadBalancer status changed and patch service if needed.
func updateServiceStatus(ctx *context.ControllerContext, svc *v1.Service, newStatus *v1.LoadBalancerStatus, logger klog.Logger) error {
	logger.V(2).Info("Updating service status", "serviceKey", klog.KRef(svc.Namespace, svc.Name), "newStatus", fmt.Sprintf("%+v", newStatus))
	if helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus) {
		logger.V(2).Info("New and old statuses are equal, skipping patch", "serviceKey", klog.KRef(svc.Namespace, svc.Name))
		return nil
	}
	return patch.PatchServiceLoadBalancerStatus(ctx.KubeClient.CoreV1(), svc, *newStatus)
}

// isHealthCheckDeleted checks if given health check exists in GCE
func isHealthCheckDeleted(cloud *gce.Cloud, hcName string, logger klog.Logger) bool {
	_, err := composite.GetHealthCheck(cloud, meta.GlobalKey(hcName), meta.VersionGA, logger)
	return utils.IsNotFoundError(err)
}

func skipUserError(err error, logger klog.Logger) error {
	if utils.IsUserError(err) {
		logger.Info("Sync failed with user-caused error", "err", err)
		return nil
	}
	return err
}
