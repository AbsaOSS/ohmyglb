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

package depresolver

import (
	"context"
	"reflect"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var predefinedStrategy = k8gbv1beta1.Strategy{
	DNSTtlSeconds:              30,
	SplitBrainThresholdSeconds: 300,
}

// ResolveGslbSpec fills Gslb by spec values. It executes always, when gslb is initialised.
// If spec value is not defined, it will use the default value. Function returns error if input is invalid.
func (dr *DependencyResolver) ResolveGslbSpec(ctx context.Context, gslb *k8gbv1beta1.Gslb, client client.Client) error {
	if !reflect.DeepEqual(gslb.Spec, dr.spec) {
		// set predefined values if missing in the yaml
		if gslb.Spec.Strategy.DNSTtlSeconds == 0 {
			gslb.Spec.Strategy.DNSTtlSeconds = predefinedStrategy.DNSTtlSeconds
		}
		if gslb.Spec.Strategy.SplitBrainThresholdSeconds == 0 {
			gslb.Spec.Strategy.SplitBrainThresholdSeconds = predefinedStrategy.SplitBrainThresholdSeconds
		}
		dr.errorSpec = dr.validateSpec(gslb.Spec.Strategy)
		if dr.errorSpec == nil {
			dr.errorSpec = client.Update(ctx, gslb)
		}
		dr.spec = gslb.Spec
	}
	return dr.errorSpec
}

func (dr *DependencyResolver) validateSpec(strategy k8gbv1beta1.Strategy) (err error) {
	err = field("DNSTtlSeconds", strategy.DNSTtlSeconds).isHigherOrEqualToZero().err
	if err != nil {
		return
	}
	err = field("SplitBrainThresholdSeconds", strategy.SplitBrainThresholdSeconds).isHigherOrEqualToZero().err
	if err != nil {
		return
	}
	return
}
