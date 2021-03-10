/*
Copyright 2021 Absa Group Limited

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
	"reflect"

	"github.com/AbsaOSS/k8gb/controllers/internal/utils"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *GslbReconciler) gslbIngress(gslb *k8gbv1beta1.Gslb) (*v1beta1.Ingress, error) {
	metav1.SetMetaDataAnnotation(&gslb.ObjectMeta, strategyAnnotation, gslb.Spec.Strategy.Type)
	if gslb.Spec.Strategy.PrimaryGeoTag != "" {
		metav1.SetMetaDataAnnotation(&gslb.ObjectMeta, primaryGeoTagAnnotation, gslb.Spec.Strategy.PrimaryGeoTag)
	}
	ingress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gslb.Name,
			Namespace:   gslb.Namespace,
			Annotations: gslb.Annotations,
		},
		Spec: gslb.Spec.Ingress,
	}

	err := controllerutil.SetControllerReference(gslb, ingress, r.Scheme)
	if err != nil {
		return nil, err
	}
	return ingress, err
}

func (r *GslbReconciler) saveIngress(instance *k8gbv1beta1.Gslb, i *v1beta1.Ingress) error {
	found := &v1beta1.Ingress{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		// Create the service
		logger.Info().Msgf("Creating a new Ingress, Ingress.Namespace %s, Ingress.Name: %s", i.Namespace, i.Name)
		err = r.Create(context.TODO(), i)

		if err != nil {
			// Creation failed
			logger.Err(err).Msgf("Failed to create new Ingress Ingress.Namespace: %s, Ingress.Name: %s",
				i.Namespace, i.Name)
			return err
		}
		// Creation was successful
		return nil
	} else if err != nil {
		// Error that isn't due to the service not existing
		logger.Err(err).Msg("Failed to get Ingress")
		return err
	}

	// Update existing object with new spec and annotations
	if !ingressEqual(found, i) {
		found.Spec = i.Spec
		found.Annotations = utils.MergeAnnotations(found.Annotations, i.Annotations)
		err = r.Update(context.TODO(), found)
		if errors.IsConflict(err) {
			logger.Info().Msgf("Ingress has been modified outside of controller, retrying reconciliation"+
				"Ingress.Namespace %s, Ingress.Name: %s", found.Namespace, found.Name)
			return nil
		}
		if err != nil {
			// Update failed
			logger.Err(err).Msgf("Failed to update Ingress Ingress.Namespace %s, Ingress.Name: %s",
				found.Namespace, found.Name)
			return err
		}
	}

	return nil
}

func ingressEqual(ing1 *v1beta1.Ingress, ing2 *v1beta1.Ingress) bool {
	for k, v := range ing2.Annotations {
		if ing1.Annotations[k] != v {
			return false
		}
	}
	return reflect.DeepEqual(ing1.Spec, ing2.Spec)
}
