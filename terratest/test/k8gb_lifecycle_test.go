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

package test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	//k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/stretchr/testify/require"
)

// TestK8gbRepeatedlyRecreatedFromIngress creates GSLB, than keeps operator live and than recreates GSLB again from Ingress.
// This is usual lifecycle scenario and we are testing spec strategy has expected values.
func TestK8gbRepeatedlyRecreatedFromIngress(t *testing.T) {
	t.Parallel()
	// name of ingress and gslb
	const name = "test-gslb-failover-simple"

	assertStrategy := func(t *testing.T, options *k8s.KubectlOptions) {
		assertGslbSpec(t, options, name, "spec.strategy.splitBrainThresholdSeconds", "300")
		assertGslbSpec(t, options, name, "spec.strategy.dnsTtlSeconds", "30")
		assertGslbSpec(t, options, name, "spec.strategy.primaryGeoTag", "eu")
		assertGslbSpec(t, options, name, "spec.strategy.type", "failover")
	}

	// Path to the Kubernetes resource config we will test
	ingressResourcePath, err := filepath.Abs("../examples/ingress-annotation-failover-simple.yaml")
	require.NoError(t, err)

	// To ensure we can reuse the resource config on the same cluster to test different scenarios, we setup a unique
	// namespace for the resources for this test.
	// Note that namespaces must be lowercase.
	namespaceName := fmt.Sprintf("k8gb-basic-example-%s", strings.ToLower(random.UniqueId()))

	// Here we choose to use the defaults, which is:
	// - HOME/.kube/config for the kubectl config file
	// - Current context of the kubectl config file
	// - Random namespace
	options := k8s.NewKubectlOptions("", "", namespaceName)

	k8s.CreateNamespace(t, options, namespaceName)

	defer k8s.DeleteNamespace(t, options, namespaceName)

	defer k8s.KubectlDelete(t, options, ingressResourcePath)

	k8s.KubectlApply(t, options, ingressResourcePath)

	k8s.WaitUntilIngressAvailable(t, options, name, 60, 1*time.Second)

	ingress := k8s.GetIngress(t, options, name)

	require.Equal(t, ingress.Name, name)

	// assert Gslb strategy has expected values
	assertStrategy(t, options)

	k8s.KubectlDelete(t, options, ingressResourcePath)

	err = k8s.RunKubectlE(t, options, "delete", "gslb", name)

	require.NoError(t, err)

	// recreate ingress
	k8s.KubectlApply(t, options, ingressResourcePath)

	k8s.WaitUntilIngressAvailable(t, options, name, 60, 1*time.Second)

	ingress = k8s.GetIngress(t, options, name)

	require.Equal(t, ingress.Name, name)
	// assert Gslb strategy has expected values
	assertStrategy(t, options)
}

// TestK8gbSpecKeepsStableAfterIngressUpdates, If ingress is updated and GSLB has non default values, the GSLB stays
// stable and is not updated.
func TestK8gbSpecKeepsStableAfterIngressUpdates(t *testing.T) {
	t.Parallel()
	// name of ingress and gslb
	const name = "test-gslb-failover-simple"

	assertStrategy := func(t *testing.T, options *k8s.KubectlOptions) {
		assertGslbSpec(t, options, name, "spec.strategy.splitBrainThresholdSeconds", "600")
		assertGslbSpec(t, options, name, "spec.strategy.dnsTtlSeconds", "60")
		assertGslbSpec(t, options, name, "spec.strategy.primaryGeoTag", "eu")
		assertGslbSpec(t, options, name, "spec.strategy.type", "failover")
	}

	kubeResourcePath, err := filepath.Abs("../examples/failover-spec.yaml")
	ingressResourcePath, err := filepath.Abs("../examples/ingress-annotation-failover.yaml")
	require.NoError(t, err)
	// To ensure we can reuse the resource config on the same cluster to test different scenarios, we setup a unique
	// namespace for the resources for this test.
	// Note that namespaces must be lowercase.
	namespaceName := fmt.Sprintf("k8gb-test-%s", strings.ToLower(random.UniqueId()))

	// Here we choose to use the defaults, which is:
	// - HOME/.kube/config for the kubectl config file
	// - Current context of the kubectl config file
	// - Random namespace
	options := k8s.NewKubectlOptions("", "", namespaceName)

	k8s.CreateNamespace(t, options, namespaceName)
	defer k8s.DeleteNamespace(t, options, namespaceName)

	// create gslb
	k8s.KubectlApply(t, options, kubeResourcePath)
	k8s.WaitUntilIngressAvailable(t, options, name, 60, 1*time.Second)

	assertStrategy(t, options)

	// reapply ingress
	k8s.KubectlApply(t, options, ingressResourcePath)

	k8s.WaitUntilIngressAvailable(t, options, name, 60, 1*time.Second)

	ingress := k8s.GetIngress(t, options, name)

	require.Equal(t, ingress.Name, name)
	// assert Gslb strategy has initial values, ingress doesn't change it
	assertStrategy(t, options)
}
