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

//package test
//
//import (
//	"fmt"
//	"path/filepath"
//	"strings"
//	"testing"
//	"time"
//
//	"github.com/stretchr/testify/assert"
//	"github.com/stretchr/testify/require"
//
//	"github.com/gruntwork-io/terratest/modules/k8s"
//	"github.com/gruntwork-io/terratest/modules/random"
//)
//
//// Basic k8gb deployment test that is verifying that associated ingress is getting created
//// Relies on two local clusters deployed by `$make deploy-two-local-clusters`
//// Tests expected behavior for https://github.com/AbsaOSS/k8gb/issues/67
//func TestK8gbSplitFailoverExample(t *testing.T) {
//	t.Parallel()
//
//	// Path to the Kubernetes resource config we will test
//	kubeResourcePath1, err := filepath.Abs("../examples/failover1.yaml")
//	require.NoError(t, err)
//
//	kubeResourcePath2, err := filepath.Abs("../examples/failover2.yaml")
//	require.NoError(t, err)
//
//	// To ensure we can reuse the resource config on the same cluster to test different scenarios, we setup a unique
//	// namespace for the resources for this test.
//	// Note that namespaces must be lowercase.
//	namespaceName := fmt.Sprintf("k8gb-test-%s", strings.ToLower(random.UniqueId()))
//
//	// Here we choose to use the defaults, which is:
//	// - HOME/.kube/config for the kubectl config file
//	// - Current context of the kubectl config file
//	// - Random namespace
//	optionsContext1 := k8s.NewKubectlOptions("k3d-test-gslb1", "", namespaceName)
//	optionsContext2 := k8s.NewKubectlOptions("k3d-test-gslb2", "", namespaceName)
//
//	k8s.CreateNamespace(t, optionsContext1, namespaceName)
//	k8s.CreateNamespace(t, optionsContext2, namespaceName)
//	defer k8s.DeleteNamespace(t, optionsContext1, namespaceName)
//	defer k8s.DeleteNamespace(t, optionsContext2, namespaceName)
//
//	gslbName := "test-gslb"
//
//	createGslbWithHealthyApp(t, optionsContext1, kubeResourcePath1, gslbName, "terratest-failover-split.cloud.example.com")
//
//	createGslbWithHealthyApp(t, optionsContext2, kubeResourcePath2, gslbName, "terratest-failover-split.cloud.example.com")
//
//	expectedIPsCluster1 := GetIngressIPs(t, optionsContext1, gslbName)
//	expectedIPsCluster2 := GetIngressIPs(t, optionsContext2, gslbName)
//
//	t.Run("Each cluster resolves its own set of IP addresses", func(t *testing.T) {
//		beforeFailoverResponseCluster1, err := DoWithRetryWaitingForValueE(
//			t,
//			"Wait 1st cluster coredns to pickup dns values...",
//			300,
//			1*time.Second,
//			func() ([]string, error) {
//				return Dig(t, "localhost", 5053, "terratest-failover-split.cloud.example.com")
//			},
//			expectedIPsCluster1)
//		require.NoError(t, err)
//
//		assert.Equal(t, beforeFailoverResponseCluster1, expectedIPsCluster1)
//
//		beforeFailoverResponseCluster2, err := DoWithRetryWaitingForValueE(
//			t,
//			"Wait 2nd cluster coredns to pickup dns values...",
//			300,
//			1*time.Second,
//			func() ([]string, error) {
//				return Dig(t, "localhost", 5054, "terratest-failover-split.cloud.example.com")
//			},
//			expectedIPsCluster2)
//		require.NoError(t, err)
//
//		assert.Equal(t, beforeFailoverResponseCluster2, expectedIPsCluster2)
//	})
//
//	t.Run("serviceHealth becomes Unhealthy after scaling down to 0", func(t *testing.T) {
//
//		k8s.RunKubectl(t, optionsContext1, "scale", "deploy", "frontend-podinfo", "--replicas=0")
//
//		assertGslbStatus(t, optionsContext1, gslbName, "terratest-failover-split.cloud.example.com:Unhealthy")
//	})
//
//	t.Run("Cluster 1 failovers to Cluster 2", func(t *testing.T) {
//
//		afterFailoverResponse, err := DoWithRetryWaitingForValueE(
//			t,
//			"Wait for failover to happen and coredns to pickup new values(cluster1)...",
//			300,
//			1*time.Second,
//			func() ([]string, error) {
//				return Dig(t, "localhost", 5053, "terratest-failover-split.cloud.example.com")
//			},
//			expectedIPsCluster2)
//		require.NoError(t, err)
//
//		assert.Equal(t, afterFailoverResponse, expectedIPsCluster2)
//	})
//
//	t.Run("Cluster 2 still returns own entries", func(t *testing.T) {
//
//		afterFailoverResponse, err := DoWithRetryWaitingForValueE(
//			t,
//			"Wait for failover to happen and coredns to pickup new values(cluster2)...",
//			300,
//			1*time.Second,
//			func() ([]string, error) {
//				return Dig(t, "localhost", 5054, "terratest-failover-split.cloud.example.com")
//			},
//			expectedIPsCluster2)
//		require.NoError(t, err)
//
//		assert.Equal(t, afterFailoverResponse, expectedIPsCluster2)
//	})
//
//	t.Run("serviceHealth becomes Healthy after scaling up", func(t *testing.T) {
//
//		k8s.RunKubectl(t, optionsContext1, "scale", "deploy", "frontend-podinfo", "--replicas=1")
//
//		assertGslbStatus(t, optionsContext1, gslbName, "terratest-failover-split.cloud.example.com:Healthy")
//	})
//
//	t.Run("Cluster 1 returns own entries again", func(t *testing.T) {
//
//		afterFailoverResponse, err := DoWithRetryWaitingForValueE(
//			t,
//			"Wait for failover to happen and coredns to pickup new values(cluster1)...",
//			300,
//			1*time.Second,
//			func() ([]string, error) {
//				return Dig(t, "localhost", 5053, "terratest-failover-split.cloud.example.com")
//			},
//			expectedIPsCluster1)
//		require.NoError(t, err)
//
//		assert.Equal(t, afterFailoverResponse, expectedIPsCluster1)
//	})
//
//}
