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

package utils

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

type ReconcileResultHandler struct {
	delayedResult ctrl.Result
}

func NewReconcileResultHandler(reconcileAfter int) *ReconcileResultHandler {
	return &ReconcileResultHandler{
		delayedResult: ctrl.Result{RequeueAfter: time.Second * time.Duration(reconcileAfter)},
	}
}

// Stop stops reconciliation loop
func (r *ReconcileResultHandler) Stop() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// RequeueError requeue loop immediately
// see default controller limiter: https://danielmangum.com/posts/controller-runtime-client-go-rate-limiting/
func (r *ReconcileResultHandler) RequeueError(err error) (ctrl.Result, error) {
	// logging error is handled in caller function
	return ctrl.Result{}, err
}

// Requeue requeue loop after config.ReconcileRequeueSeconds
// this apply in case you didn't modify request resources.
// If so, reconciliation starts immediately
// see: https://github.com/operator-framework/operator-sdk/issues/1164
func (r *ReconcileResultHandler) Requeue() (ctrl.Result, error) {
	return r.delayedResult, nil
}

func (r *ReconcileResultHandler) RequeueNow() (ctrl.Result, error) {
	return ctrl.Result{Requeue: true}, nil
}
