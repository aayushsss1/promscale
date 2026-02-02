package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deploymentCurrentReplicas(dep *appsv1.Deployment) int32 {
	if dep.Spec.Replicas == nil {
		return 0
	}
	return *dep.Spec.Replicas
}

func deploymentReadyReplicas(dep *appsv1.Deployment) int32 {
	return dep.Status.ReadyReplicas
}

func patchDeploymentReplicas(ctx context.Context, c client.Client, dep *appsv1.Deployment, desired int32) error {
	patch := client.MergeFrom(dep.DeepCopy())
	dep.Spec.Replicas = &desired
	return c.Patch(ctx, dep, patch)
}
