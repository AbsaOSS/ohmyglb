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
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/AbsaOSS/k8gb/controllers/internal/utils"
	"github.com/AbsaOSS/k8gb/controllers/providers/assistant"
	"github.com/AbsaOSS/k8gb/controllers/providers/dns"
	"github.com/AbsaOSS/k8gb/controllers/providers/metrics"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

type testSettings struct {
	gslb       *k8gbv1beta1.Gslb
	reconciler *GslbReconciler
	request    reconcile.Request
	config     depresolver.Config
	client     client.Client
	ingress    *v1beta1.Ingress
	finalCall  bool
	assistant  assistant.IAssistant
}

var crSampleYaml = "../deploy/crds/k8gb.absa.oss_v1beta1_gslb_cr.yaml"

var predefinedConfig = depresolver.Config{
	ReconcileRequeueSeconds: 30,
	ClusterGeoTag:           "us-west-1",
	ExtClustersGeoTags:      []string{"us-east-1"},
	EdgeDNSServer:           "8.8.8.8",
	EdgeDNSZone:             "example.com",
	DNSZone:                 "cloud.example.com",
	K8gbNamespace:           "k8gb",
	Infoblox: depresolver.Infoblox{
		Host:                "fakeinfoblox.example.com",
		Username:            "foo",
		Password:            "blah",
		Port:                443,
		Version:             "0.0.0",
		HTTPPoolConnections: 20,
		HTTPRequestTimeout:  10,
	},
	Override: depresolver.Override{
		FakeInfobloxEnabled: true,
	},
	Log: depresolver.Log{
		Format: depresolver.SimpleFormat,
	},
}

const coreDNSExtServiceName = "k8gb-coredns-lb"

func TestNotFoundServiceStatus(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	expectedServiceStatus := "NotFound"
	notFoundHost := "notfound.cloud.example.com"
	// act
	actualServiceStatus := settings.gslb.Status.ServiceHealth[notFoundHost]
	// assert
	assert.Equal(t, expectedServiceStatus, actualServiceStatus, "expected %s service status to be %s, but got %s",
		notFoundHost, expectedServiceStatus, actualServiceStatus)
}

func TestUnhealthyServiceStatus(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	serviceName := "unhealthy-app"
	unhealthyHost := "unhealthy.cloud.example.com"
	expectedServiceStatus := "Unhealthy"
	defer deleteUnhealthyService(t, &settings, serviceName)
	// act
	createUnhealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	// assert
	actualServiceStatus := settings.gslb.Status.ServiceHealth[unhealthyHost]
	assert.Equal(t, expectedServiceStatus, actualServiceStatus, "expected %s service status to be %s, but got %s",
		unhealthyHost, expectedServiceStatus, actualServiceStatus)
}

func TestHealthyServiceStatus(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	serviceName := "frontend-podinfo"
	expectedServiceStatus := "Healthy"
	healthyHost := "roundrobin.cloud.example.com"
	defer deleteHealthyService(t, &settings, serviceName)
	createHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	// act
	actualServiceStatus := settings.gslb.Status.ServiceHealth[healthyHost]
	// assert
	assert.Equal(t, expectedServiceStatus, actualServiceStatus, "expected %s service status to be %s, but got %s",
		healthyHost, expectedServiceStatus, actualServiceStatus)
}

func TestIngressHostsPerStatusMetric(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	err := settings.reconciler.Metrics.Register()
	require.NoError(t, err)
	defer settings.reconciler.Metrics.Unregister()
	expectedHostsMetricCount := 3
	// act
	ingressHostsPerStatusMetric := settings.reconciler.Metrics.GetIngressHostsPerStatusMetric()
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.gslb)
	actualHostsMetricCount := testutil.CollectAndCount(ingressHostsPerStatusMetric)
	// assert
	assert.NoError(t, err, "Failed to get expected gslb")
	assert.Equal(t, expectedHostsMetricCount, actualHostsMetricCount, "expected %v managed hosts, but got %v",
		expectedHostsMetricCount, actualHostsMetricCount)
}

func TestIngressHostsPerStatusMetricReflectionForHealthyStatus(t *testing.T) {
	// I'm running test multiple times to check that it work properly when healthy service is up and down multiple times
	defer cleanup()
	for i := 0; i < 4; i++ {
		func() {
			// arrange
			settings := provideSettings(t, predefinedConfig)
			defer settings.reconciler.Metrics.Unregister()
			err := settings.reconciler.Metrics.Register()
			require.NoError(t, err)
			serviceName := "frontend-podinfo"
			defer deleteHealthyService(t, &settings, serviceName)
			expectedHostsMetric := 1.
			createHealthyService(t, &settings, serviceName)
			reconcileAndUpdateGslb(t, settings)
			// act
			err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.gslb)
			ingressHostsPerStatusMetric := settings.reconciler.Metrics.GetIngressHostsPerStatusMetric()
			healthyHosts := ingressHostsPerStatusMetric.With(prometheus.Labels{"namespace": settings.gslb.Namespace,
				"name": settings.gslb.Name, "status": metrics.HealthyStatus})
			actualHostsMetric := testutil.ToFloat64(healthyHosts)
			// assert
			assert.NoError(t, err, "Failed to get expected gslb")
			assert.Equal(t, expectedHostsMetric, actualHostsMetric, "expected %v managed hosts with Healthy status, but got %v",
				expectedHostsMetric, actualHostsMetric)
		}()
	}
}

func TestIngressHostsPerStatusMetricReflectionForUnhealthyStatus(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	defer settings.reconciler.Metrics.Unregister()
	err := settings.reconciler.Metrics.Register()
	require.NoError(t, err)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.gslb)
	expectedHostsMetricCount := 0.
	// act
	ingressHostsPerStatusMetric := settings.reconciler.Metrics.GetIngressHostsPerStatusMetric()
	unhealthyHosts := ingressHostsPerStatusMetric.With(prometheus.Labels{"namespace": settings.gslb.Namespace,
		"name": settings.gslb.Name, "status": metrics.UnhealthyStatus})
	actualHostsMetricCount := testutil.ToFloat64(unhealthyHosts)
	// assert
	assert.NoError(t, err, "Failed to get expected gslb")
	assert.Equal(t, expectedHostsMetricCount, actualHostsMetricCount, "expected %v managed hosts, but got %v",
		expectedHostsMetricCount, actualHostsMetricCount)

	// arrange
	serviceName := "unhealthy-app"
	createUnhealthyService(t, &settings, serviceName)
	defer deleteUnhealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	expectedHostsMetricCount = 1
	// act
	unhealthyHosts =
		ingressHostsPerStatusMetric.With(prometheus.Labels{"namespace": settings.gslb.Namespace,
			"name": settings.gslb.Name, "status": metrics.UnhealthyStatus})
	actualHostsMetricCount = testutil.ToFloat64(unhealthyHosts)
	// assert
	assert.Equal(t, expectedHostsMetricCount, actualHostsMetricCount, "expected %v managed hosts with Healthy status, but got %v",
		expectedHostsMetricCount, actualHostsMetricCount)
}

func TestIngressHostsPerStatusMetricReflectionForNotFoundStatus(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	defer settings.reconciler.Metrics.Unregister()
	err := settings.reconciler.Metrics.Register()
	require.NoError(t, err)
	expectedHostsMetricCount := 2.0

	serviceName := "unhealthy-app"
	createUnhealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	deleteUnhealthyService(t, &settings, serviceName)

	// act
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.gslb)
	require.NoError(t, err, "Failed to get expected gslb")
	ingressHostsPerStatusMetric := settings.reconciler.Metrics.GetIngressHostsPerStatusMetric()
	unknownHosts, err := ingressHostsPerStatusMetric.GetMetricWith(
		prometheus.Labels{"namespace": settings.gslb.Namespace, "name": settings.gslb.Name, "status": metrics.NotFoundStatus})
	require.NoError(t, err, "Failed to get ingress metrics")
	actualHostsMetricCount := testutil.ToFloat64(unknownHosts)
	// assert
	assert.Equal(t, expectedHostsMetricCount, actualHostsMetricCount, "expected %v managed hosts with NotFound status, but got %v",
		expectedHostsMetricCount, actualHostsMetricCount)
}

func TestHealthyRecordMetric(t *testing.T) {
	// arrange
	defer cleanup()
	expectedHealthyRecordsMetricCount := 3.0
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	serviceName := "frontend-podinfo"
	settings := provideSettings(t, predefinedConfig)
	defer settings.reconciler.Metrics.Unregister()
	err := settings.reconciler.Metrics.Register()
	require.NoError(t, err)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.gslb)
	require.NoError(t, err, "Failed to get expected gslb")
	defer deleteHealthyService(t, &settings, serviceName)
	createHealthyService(t, &settings, serviceName)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")
	reconcileAndUpdateGslb(t, settings)
	// act
	healthyRecordsMetric := settings.reconciler.Metrics.GetHealthyRecordsMetric()
	actualHealthyRecordsMetricCount := testutil.ToFloat64(healthyRecordsMetric)
	reconcileAndUpdateGslb(t, settings)
	// assert
	assert.Equal(t, expectedHealthyRecordsMetricCount, actualHealthyRecordsMetricCount, "expected %v healthy records, but got %v",
		expectedHealthyRecordsMetricCount, actualHealthyRecordsMetricCount)
}

func TestMetricLinterCheck(t *testing.T) {
	// arrange
	settings := provideSettings(t, predefinedConfig)
	defer settings.reconciler.Metrics.Unregister()
	err := settings.reconciler.Metrics.Register()
	require.NoError(t, err)
	healthyRecordsMetric := settings.reconciler.Metrics.GetHealthyRecordsMetric()
	ingressHostsPerStatusMetric := settings.reconciler.Metrics.GetIngressHostsPerStatusMetric()
	for name, scenario := range map[string]prometheus.Collector{
		"healthy_records":          healthyRecordsMetric,
		"ingress_hosts_per_status": ingressHostsPerStatusMetric,
	} {
		// act
		// assert
		lintErrors, err := testutil.CollectAndLint(scenario)
		assert.NoError(t, err)
		assert.True(t, len(lintErrors) == 0, "Metric linting error(s): %s - %s", name, lintErrors)
	}
}

func TestGslbCreatesDNSEndpointCRForHealthyIngressHosts(t *testing.T) {
	// arrange
	defer cleanup()
	serviceName := "frontend-podinfo"
	dnsEndpoint := &externaldns.DNSEndpoint{}
	want := []*externaldns.Endpoint{
		{
			DNSName:    "localtargets-roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{
			DNSName:    "roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
	}
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	settings := provideSettings(t, predefinedConfig)
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)

	// act
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to load DNS endpoint")
	got := dnsEndpoint.Spec.Endpoints
	prettyGot := utils.ToString(got)
	prettyWant := utils.ToString(want)

	// assert
	assert.Equal(t, want, got, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestDNSRecordReflectionInStatus(t *testing.T) {
	// arrange
	defer cleanup()
	serviceName := "frontend-podinfo"
	dnsEndpoint := &externaldns.DNSEndpoint{}
	want := map[string][]string{"roundrobin.cloud.example.com": {"10.0.0.1", "10.0.0.2", "10.0.0.3"}}
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	settings := provideSettings(t, predefinedConfig)
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// act
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to load DNS endpoint")
	got := settings.gslb.Status.HealthyRecords

	// assert
	assert.Equal(t, got, want, "got:\n %s healthyRecordsMetric status,\n\n want:\n %s", got, want)
}

func TestLocalDNSRecordsHasSpecialAnnotation(t *testing.T) {
	// arrange
	defer cleanup()
	serviceName := "frontend-podinfo"
	dnsEndpoint := &externaldns.DNSEndpoint{}
	want := "local"
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	settings := provideSettings(t, predefinedConfig)
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// act
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to load DNS endpoint")
	got := dnsEndpoint.Annotations["k8gb.absa.oss/dnstype"]

	// assert
	assert.Equal(t, got, want, "got:\n %q annotation value,\n\n want:\n %q", got, want, got, want)
}

func TestCanGetExternalTargetsFromK8gbInAnotherLocation(t *testing.T) {
	// arrange
	defer cleanup()
	serviceName := "frontend-podinfo"
	want := []*externaldns.Endpoint{
		{
			DNSName:    "localtargets-roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{
			DNSName:    "roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.1.0.1", "10.1.0.2", "10.1.0.3"}},
	}
	hrWant := map[string][]string{"roundrobin.cloud.example.com": {"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.1.0.1", "10.1.0.2", "10.1.0.3"}}
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	dnsEndpoint := &externaldns.DNSEndpoint{}
	customConfig := predefinedConfig
	customConfig.Override.FakeDNSEnabled = true
	settings := provideSettings(t, customConfig)

	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// act
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")

	got := dnsEndpoint.Spec.Endpoints
	hrGot := settings.gslb.Status.HealthyRecords
	prettyGot := utils.ToString(got)
	prettyWant := utils.ToString(want)

	// assert
	assert.Equal(t, want, got, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
	assert.Equal(t, hrGot, hrWant, "got:\n %s Gslb Records status,\n\n want:\n %s", hrGot, hrWant)
}

func TestCanCheckExternalGslbTXTRecordForValidityAndFailIfItIsExpired(t *testing.T) {
	// arrange
	defer cleanup()
	customConfig := predefinedConfig
	customConfig.Override.FakeDNSEnabled = true
	customConfig.EdgeDNSServer = "fake"
	settings := provideSettings(t, customConfig)
	// act
	got := settings.assistant.InspectTXTThreshold("test-gslb-heartbeat-eu.example.com",
		customConfig.Override.FakeDNSEnabled, time.Minute*5)
	want := errors.NewResourceExpired("Split brain TXT record expired the time threshold: (5m0s)")
	// assert
	assert.Equal(t, want, got, "got:\n %s from TXT split brain check,\n\n want error:\n %v", got, want)
}

func TestCanCheckExternalGslbTXTRecordForValidityAndPAssIfItISNotExpired(t *testing.T) {
	// arrange
	customConfig := predefinedConfig
	customConfig.Override.FakeDNSEnabled = true
	customConfig.EdgeDNSServer = "fake"
	settings := provideSettings(t, customConfig)
	// act
	err2 := settings.assistant.InspectTXTThreshold("test-gslb-heartbeat-za.example.com",
		customConfig.Override.FakeDNSEnabled, time.Minute*5)
	// assert
	assert.NoError(t, err2, "got:\n %s from TXT split brain check,\n\n want error:\n %v", err2, nil)
}

func TestReturnsOwnRecordsUsingFailoverStrategyWhenPrimary(t *testing.T) {
	defer cleanup()
	serviceName := "frontend-podinfo"
	want := []*externaldns.Endpoint{
		{
			DNSName:    "localtargets-roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			DNSName:    "roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
	}
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	dnsEndpoint := &externaldns.DNSEndpoint{}
	customConfig := predefinedConfig
	customConfig.ClusterGeoTag = "eu"
	customConfig.Override.FakeDNSEnabled = true
	settings := provideSettings(t, customConfig)

	// ingress
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// enable failover strategy
	settings.gslb.Spec.Strategy.Type = "failover"
	settings.gslb.Spec.Strategy.PrimaryGeoTag = "eu"
	err = settings.client.Update(context.TODO(), settings.gslb)
	require.NoError(t, err, "Can't update gslb")

	// act
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")
	got := dnsEndpoint.Spec.Endpoints
	prettyGot := utils.ToString(got)
	prettyWant := utils.ToString(want)

	// assert
	assert.Equal(t, want, got, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestReturnsExternalRecordsUsingFailoverStrategy(t *testing.T) {
	// arrange
	serviceName := "frontend-podinfo"
	want := []*externaldns.Endpoint{
		{
			DNSName:    "localtargets-roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		},
		{
			DNSName:    "roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"10.1.0.1", "10.1.0.2", "10.1.0.3"},
		},
	}
	ingressIPs := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	dnsEndpoint := &externaldns.DNSEndpoint{}
	customConfig := predefinedConfig
	customConfig.ClusterGeoTag = "za"
	customConfig.Override.FakeDNSEnabled = true
	settings := provideSettings(t, customConfig)

	// ingress
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")
	settings.ingress.Status.LoadBalancer.Ingress = append(settings.ingress.Status.LoadBalancer.Ingress, ingressIPs...)
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// enable failover strategy
	settings.gslb.Spec.Strategy.Type = "failover"
	settings.gslb.Spec.Strategy.PrimaryGeoTag = "eu"
	err = settings.client.Update(context.TODO(), settings.gslb)
	require.NoError(t, err, "Can't update gslb")

	// act
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")
	got := dnsEndpoint.Spec.Endpoints
	prettyGot := utils.ToString(got)
	prettyWant := utils.ToString(want)

	// assert
	assert.Equal(t, want, got, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestGslbProperlyPropagatesAnnotationDownToIngress(t *testing.T) {
	// arrange
	defer cleanup()
	settings := provideSettings(t, predefinedConfig)
	expectedAnnotations := map[string]string{"annotation": "test"}
	settings.gslb.Annotations = expectedAnnotations
	err := settings.client.Update(context.TODO(), settings.gslb)
	require.NoError(t, err, "Can't update gslb")
	// act
	reconcileAndUpdateGslb(t, settings)
	err2 := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	// assert
	assert.NoError(t, err2, "Failed to get expected ingress")
	assert.Equal(t, expectedAnnotations, settings.ingress.Annotations)
}

func TestReflectGeoTagInStatusAsUnsetByDefault(t *testing.T) {
	// arrange
	defer cleanup()
	want := "us-west-1"
	settings := provideSettings(t, predefinedConfig)
	// act
	reconcileAndUpdateGslb(t, settings)
	got := settings.gslb.Status.GeoTag
	// assert
	assert.Equal(t, want, got, "got: '%s' GeoTag status, want:'%s'", got, want)
}

func TestReflectGeoTagInTheStatus(t *testing.T) {
	// arrange
	defer cleanup()
	want := "eu"
	customConfig := predefinedConfig
	customConfig.ClusterGeoTag = "eu"
	settings := provideSettings(t, customConfig)
	// act
	reconcileAndUpdateGslb(t, settings)
	got := settings.gslb.Status.GeoTag
	// assert
	assert.Equal(t, want, got, "got: '%s' GeoTag status, want:'%s'", got, want)
}

func TestDetectsIngressHostnameMismatch(t *testing.T) {
	// arrange
	defer cleanup()
	// getting Gslb and Reconciler
	predefinedSettings := provideSettings(t, predefinedConfig)
	customConfig := predefinedConfig
	customConfig.EdgeDNSZone = "otherdnszone.com"
	predefinedSettings.config = customConfig
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      predefinedSettings.gslb.Name,
			Namespace: predefinedSettings.gslb.Namespace,
		},
	}
	// injecting custom config to reconciler created from predefined config
	predefinedSettings.reconciler.Config = &customConfig
	// act
	_, err := predefinedSettings.reconciler.Reconcile(req)
	// assert
	assert.Error(t, err, "expected controller to detect Ingress hostname and edgeDNSZone mismatch")
	assert.True(t, strings.HasSuffix(err.Error(), "cloud.example.com does not match delegated zone otherdnszone.com"))
}

func TestCreatesNSDNSRecordsForRoute53(t *testing.T) {
	// arrange
	defer cleanup()
	const dnsZone = "cloud.example.com"
	const want = "route53"
	wantEp := []*externaldns.Endpoint{
		{
			DNSName:    dnsZone,
			RecordTTL:  30,
			RecordType: "NS",
			Targets: externaldns.Targets{
				"gslb-ns-cloud-example-com-eu.example.com",
				"gslb-ns-cloud-example-com-us.example.com",
				"gslb-ns-cloud-example-com-za.example.com",
			},
		},
		{
			DNSName:    "gslb-ns-cloud-example-com-eu.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets: externaldns.Targets{
				"1.0.0.1",
				"1.1.1.1",
			},
		},
	}
	dnsEndpointRoute53 := &externaldns.DNSEndpoint{}
	customConfig := predefinedConfig
	customConfig.EdgeDNSServer = "1.1.1.1"
	customConfig.CoreDNSExposed = true
	coreDNSService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreDNSExtServiceName,
			Namespace: predefinedConfig.K8gbNamespace,
		},
	}
	serviceIPs := []corev1.LoadBalancerIngress{
		{Hostname: "one.one.one.one"}, // rely on 1.1.1.1 response from Cloudflare
	}
	settings := provideSettings(t, customConfig)
	err := settings.client.Create(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to create testing %s service", coreDNSExtServiceName)
	coreDNSService.Status.LoadBalancer.Ingress = append(coreDNSService.Status.LoadBalancer.Ingress, serviceIPs...)
	err = settings.client.Status().Update(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to update coredns service lb hostname")

	// act
	customConfig.EdgeDNSType = depresolver.DNSTypeRoute53
	customConfig.ClusterGeoTag = "eu"
	customConfig.ExtClustersGeoTags = []string{"za", "us"}
	customConfig.DNSZone = dnsZone
	// apply new environment variables and update config only
	settings.reconciler.Config = &customConfig
	// If config is changed, new Route53 provider needs to be re-created. There is no way and reason to change provider
	// configuration at another time than startup
	f, _ := dns.NewDNSProviderFactory(settings.reconciler.Client, customConfig)
	settings.reconciler.DNSProvider = f.Provider()

	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), client.ObjectKey{Namespace: predefinedConfig.K8gbNamespace, Name: "k8gb-ns-route53"}, dnsEndpointRoute53)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")
	got := dnsEndpointRoute53.Annotations["k8gb.absa.oss/dnstype"]
	gotEp := dnsEndpointRoute53.Spec.Endpoints
	prettyGot := utils.ToString(gotEp)
	prettyWant := utils.ToString(wantEp)

	// assert
	assert.Equal(t, want, got, "got:\n %q annotation value,\n\n want:\n %q", got, want)
	assert.Equal(t, wantEp, gotEp, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestCreatesNSDNSRecordsForNS1(t *testing.T) {
	// arrange
	defer cleanup()
	const dnsZone = "cloud.example.com"
	const want = "ns1"
	wantEp := []*externaldns.Endpoint{
		{
			DNSName:    dnsZone,
			RecordTTL:  30,
			RecordType: "NS",
			Targets: externaldns.Targets{
				"gslb-ns-cloud-example-com-eu.example.com",
				"gslb-ns-cloud-example-com-us.example.com",
				"gslb-ns-cloud-example-com-za.example.com",
			},
		},
		{
			DNSName:    "gslb-ns-cloud-example-com-eu.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets: externaldns.Targets{
				"1.0.0.1",
				"1.1.1.1",
			},
		},
	}
	dnsEndpointNS1 := &externaldns.DNSEndpoint{}
	customConfig := predefinedConfig
	customConfig.EdgeDNSServer = "1.1.1.1"
	customConfig.CoreDNSExposed = true
	coreDNSService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreDNSExtServiceName,
			Namespace: predefinedConfig.K8gbNamespace,
		},
	}
	serviceIPs := []corev1.LoadBalancerIngress{
		{Hostname: "one.one.one.one"}, // rely on 1.1.1.1 response from Cloudflare
	}
	settings := provideSettings(t, customConfig)
	err := settings.client.Create(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to create testing %s service", coreDNSExtServiceName)
	coreDNSService.Status.LoadBalancer.Ingress = append(coreDNSService.Status.LoadBalancer.Ingress, serviceIPs...)
	err = settings.client.Status().Update(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to update coredns service lb hostname")

	// act
	customConfig.EdgeDNSType = depresolver.DNSTypeNS1
	customConfig.ClusterGeoTag = "eu"
	customConfig.ExtClustersGeoTags = []string{"za", "us"}
	customConfig.DNSZone = dnsZone
	// apply new environment variables and update config only
	settings.reconciler.Config = &customConfig
	// If config is changed, new Route53 provider needs to be re-created. There is no way and reason to change provider
	// configuration at another time than startup
	f, _ := dns.NewDNSProviderFactory(settings.reconciler.Client, customConfig)
	settings.reconciler.DNSProvider = f.Provider()

	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), client.ObjectKey{Namespace: predefinedConfig.K8gbNamespace, Name: "k8gb-ns-ns1"}, dnsEndpointNS1)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")
	got := dnsEndpointNS1.Annotations["k8gb.absa.oss/dnstype"]
	gotEp := dnsEndpointNS1.Spec.Endpoints
	prettyGot := utils.ToString(gotEp)
	prettyWant := utils.ToString(wantEp)

	// assert
	assert.Equal(t, want, got, "got:\n %q annotation value,\n\n want:\n %q", got, want)
	assert.Equal(t, wantEp, gotEp, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestResolvesLoadBalancerHostnameFromIngressStatus(t *testing.T) {
	// arrange
	defer cleanup()
	serviceName := "frontend-podinfo"
	want := []*externaldns.Endpoint{
		{
			DNSName:    "localtargets-roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"1.0.0.1", "1.1.1.1"}},
		{
			DNSName:    "roundrobin.cloud.example.com",
			RecordTTL:  30,
			RecordType: "A",
			Targets:    externaldns.Targets{"1.0.0.1", "1.1.1.1"}},
	}
	settings := provideSettings(t, predefinedConfig)
	dnsEndpoint := &externaldns.DNSEndpoint{ObjectMeta: metav1.ObjectMeta{Namespace: settings.gslb.Namespace, Name: settings.gslb.Name}}
	createHealthyService(t, &settings, serviceName)
	defer deleteHealthyService(t, &settings, serviceName)
	err := settings.client.Get(context.TODO(), settings.request.NamespacedName, settings.ingress)
	require.NoError(t, err, "Failed to get expected ingress")

	settings.ingress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: "one.one.one.one"}}
	err = settings.client.Status().Update(context.TODO(), settings.ingress)
	require.NoError(t, err, "Failed to update gslb Ingress Address")

	// act
	err = settings.client.Delete(context.Background(), dnsEndpoint)
	require.NoError(t, err, "Failed to update DNSEndpoint")
	reconcileAndUpdateGslb(t, settings)
	err = settings.client.Get(context.TODO(), settings.request.NamespacedName, dnsEndpoint)
	require.NoError(t, err, "Failed to get expected DNSEndpoint")
	got := dnsEndpoint.Spec.Endpoints
	prettyGot := utils.ToString(got)
	prettyWant := utils.ToString(want)

	// assert
	assert.Equal(t, want, got, "got:\n %s DNSEndpoint,\n\n want:\n %s", prettyGot, prettyWant)
}

func TestRoute53ZoneDelegationGarbageCollection(t *testing.T) {
	// arrange
	defer cleanup()

	customConfig := predefinedConfig
	settings := provideSettings(t, customConfig)
	coreDNSService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreDNSExtServiceName,
			Namespace: predefinedConfig.K8gbNamespace,
		},
	}
	serviceIPs := []corev1.LoadBalancerIngress{
		{Hostname: "one.one.one.one"}, // rely on 1.1.1.1 response from Cloudflare
	}
	err := settings.client.Create(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to create testing %s service", coreDNSExtServiceName)
	coreDNSService.Status.LoadBalancer.Ingress = append(coreDNSService.Status.LoadBalancer.Ingress, serviceIPs...)
	err = settings.client.Status().Update(context.TODO(), coreDNSService)
	require.NoError(t, err, "Failed to update coredns service lb hostname")

	// act
	customConfig.EdgeDNSType = depresolver.DNSTypeRoute53
	// apply new environment variables and update config only
	settings.reconciler.Config = &customConfig
	reconcileAndUpdateGslb(t, settings)

	deletionTimestamp := metav1.Now()
	settings.gslb.SetDeletionTimestamp(&deletionTimestamp)
	err = settings.client.Update(context.Background(), settings.gslb)
	require.NoError(t, err, "Failed to update Gslb")
	settings.finalCall = true // Gslb is about to be deleted, no requeue expected
	reconcileAndUpdateGslb(t, settings)

	// assert
	dnsEndpointRoute53 := &externaldns.DNSEndpoint{}
	err = settings.client.Get(context.TODO(), client.ObjectKey{Namespace: predefinedConfig.K8gbNamespace, Name: "k8gb-ns-route53"}, dnsEndpointRoute53)
	require.Error(t, err, "k8gb-ns-route53 DNSEndpoint should be garbage collected")
}

func TestGslbSetsAnnotationsOnTheIngress(t *testing.T) {
	// arrange
	defer cleanup()

	settings := provideSettings(t, predefinedConfig)

	// act
	reconcileAndUpdateGslb(t, settings)

	// assert
	ingress := &v1beta1.Ingress{}
	err := settings.client.Get(context.Background(), client.ObjectKey{Namespace: settings.gslb.Namespace, Name: settings.gslb.Name}, ingress)
	require.NoError(t, err, "Gslb should be created from annotated Ingress")

	assert.Equal(t, map[string]string{strategyAnnotation: "roundRobin"}, ingress.Annotations)
}

func TestMain(m *testing.M) {
	// setup tests
	fakeDNS()
	// run tests
	exitVal := m.Run()
	// teardown
	os.Exit(exitVal)
}

func createHealthyService(t *testing.T, s *testSettings, serviceName string) {
	t.Helper()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}
	err := s.client.Create(context.TODO(), service)
	if err != nil {
		t.Fatalf("Failed to create testing service: (%v)", err)
	}

	// Create fake endpoint with populated address slice
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{{IP: "1.2.3.4"}},
			},
		},
	}

	err = s.client.Create(context.TODO(), endpoint)
	if err != nil {
		t.Fatalf("Failed to create testing endpoint: (%v)", err)
	}
}

func deleteHealthyService(t *testing.T, s *testSettings, serviceName string) {
	t.Helper()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}
	err := s.client.Delete(context.TODO(), service)
	if err != nil {
		t.Fatalf("Failed to delete testing service: (%v)", err)
	}

	// Create fake endpoint with populated address slice
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{{IP: "1.2.3.4"}},
			},
		},
	}

	err = s.client.Delete(context.TODO(), endpoint)
	if err != nil {
		t.Fatalf("Failed to delete testing endpoint: (%v)", err)
	}
}

func createUnhealthyService(t *testing.T, s *testSettings, serviceName string) {
	t.Helper()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}

	err := s.client.Create(context.TODO(), service)
	if err != nil {
		t.Fatalf("Failed to create testing service: (%v)", err)
	}

	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}

	err = s.client.Create(context.TODO(), endpoint)
	if err != nil {
		t.Fatalf("Failed to create testing endpoint: (%v)", err)
	}
}

func deleteUnhealthyService(t *testing.T, s *testSettings, serviceName string) {
	t.Helper()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}

	err := s.client.Delete(context.TODO(), service)
	if err != nil {
		t.Fatalf("Failed to delete testing service: (%v)", err)
	}

	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: s.gslb.Namespace,
		},
	}

	err = s.client.Delete(context.TODO(), endpoint)
	if err != nil {
		t.Fatalf("Failed to delete testing endpoint: (%v)", err)
	}

}

func reconcileAndUpdateGslb(t *testing.T, s testSettings) {
	t.Helper()
	// Reconcile again so Reconcile() checks services and updates the Gslb
	// resources' Status.
	res, err := s.reconciler.Reconcile(s.request)
	if err != nil {
		return
	}

	if !s.finalCall {
		if res != (reconcile.Result{RequeueAfter: time.Second * 30}) {
			t.Error("reconcile did not return Result with Requeue")
		}
	}

	err = s.client.Get(context.TODO(), s.request.NamespacedName, s.gslb)
	if err != nil {
		t.Fatalf("Failed to get expected gslb: (%v)", err)
	}
}

func provideSettings(t *testing.T, expected depresolver.Config) (settings testSettings) {
	configureEnvVar(expected)
	_, err := os.Stat(crSampleYaml)
	if os.IsNotExist(err) {
		t.Fatalf("Sample CR yaml file not found at: %s", crSampleYaml)
	}
	gslbYaml, err := ioutil.ReadFile(crSampleYaml)
	if err != nil {
		t.Fatalf("Can't open example CR file: %s", crSampleYaml)
	}
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
	gslb, err := utils.YamlToGslb(gslbYaml)
	if err != nil {
		t.Fatal(err)
	}
	objs := []runtime.Object{
		gslb,
	}
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(k8gbv1beta1.GroupVersion, gslb)
	// Register external-dns DNSEndpoint CRD
	s.AddKnownTypes(schema.GroupVersion{Group: "externaldns.k8s.io", Version: "v1alpha1"}, &externaldns.DNSEndpoint{})
	// Create a fake client to mock API calls.
	cl := fake.NewFakeClientWithScheme(s, objs...)
	// Create config
	config, err := depresolver.NewDependencyResolver().ResolveOperatorConfig()
	if err != nil {
		t.Fatalf("config error: (%v)", err)
	}
	// Create a GslbReconciler object with the scheme and fake client.
	r := &GslbReconciler{
		Client: cl,
		Scheme: s,
	}
	r.DepResolver = depresolver.NewDependencyResolver()
	r.Config = config
	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	r.Metrics = metrics.NewPrometheusMetrics(*config)
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      gslb.Name,
			Namespace: gslb.Namespace,
		},
	}

	var f *dns.ProviderFactory
	f, err = dns.NewDNSProviderFactory(r.Client, *r.Config)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	r.DNSProvider = f.Provider()
	a := assistant.NewGslbAssistant(r.Client, r.Config.K8gbNamespace, r.Config.EdgeDNSServer)
	res, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	if res.Requeue {
		t.Error("requeue expected")
	}
	ingress := &v1beta1.Ingress{}
	err = cl.Get(context.TODO(), req.NamespacedName, ingress)
	if err != nil {
		t.Fatalf("Failed to get expected ingress: (%v)", err)
	}

	// Reconcile again so Reconcile() checks services and updates the Gslb
	// resources' Status.
	settings = testSettings{
		gslb:       gslb,
		config:     expected,
		reconciler: r,
		request:    req,
		client:     cl,
		ingress:    ingress,
		finalCall:  false,
		assistant:  a,
	}
	reconcileAndUpdateGslb(t, settings)
	return settings
}

func cleanup() {
	for _, s := range []string{depresolver.ReconcileRequeueSecondsKey, depresolver.ClusterGeoTagKey, depresolver.ExtClustersGeoTagsKey,
		depresolver.EdgeDNSZoneKey, depresolver.DNSZoneKey, depresolver.EdgeDNSServerKey, depresolver.K8gbNamespaceKey,
		depresolver.Route53EnabledKey, depresolver.InfobloxGridHostKey, depresolver.InfobloxVersionKey, depresolver.InfobloxPortKey,
		depresolver.InfobloxUsernameKey, depresolver.InfobloxPasswordKey, depresolver.InfobloxHTTPRequestTimeoutKey,
		depresolver.InfobloxHTTPPoolConnectionsKey, depresolver.OverrideWithFakeDNSKey, depresolver.OverrideFakeInfobloxKey,
		depresolver.LogLevelKey, depresolver.LogFormatKey, depresolver.LogNoColorKey} {
		if os.Unsetenv(s) != nil {
			panic(fmt.Errorf("cleanup %s", s))
		}
	}
}

func configureEnvVar(config depresolver.Config) {
	_ = os.Setenv(depresolver.ReconcileRequeueSecondsKey, strconv.Itoa(config.ReconcileRequeueSeconds))
	_ = os.Setenv(depresolver.ClusterGeoTagKey, config.ClusterGeoTag)
	_ = os.Setenv(depresolver.ExtClustersGeoTagsKey, strings.Join(config.ExtClustersGeoTags, ","))
	_ = os.Setenv(depresolver.EdgeDNSServerKey, config.EdgeDNSServer)
	_ = os.Setenv(depresolver.EdgeDNSZoneKey, config.EdgeDNSZone)
	_ = os.Setenv(depresolver.DNSZoneKey, config.DNSZone)
	_ = os.Setenv(depresolver.K8gbNamespaceKey, config.K8gbNamespace)
	_ = os.Setenv(depresolver.Route53EnabledKey, strconv.FormatBool(config.EdgeDNSType == depresolver.DNSTypeRoute53))
	_ = os.Setenv(depresolver.InfobloxGridHostKey, config.Infoblox.Host)
	_ = os.Setenv(depresolver.InfobloxVersionKey, config.Infoblox.Version)
	_ = os.Setenv(depresolver.InfobloxPortKey, strconv.Itoa(config.Infoblox.Port))
	_ = os.Setenv(depresolver.InfobloxUsernameKey, config.Infoblox.Username)
	_ = os.Setenv(depresolver.InfobloxPasswordKey, config.Infoblox.Password)
	_ = os.Setenv(depresolver.InfobloxHTTPRequestTimeoutKey, strconv.Itoa(config.Infoblox.HTTPRequestTimeout))
	_ = os.Setenv(depresolver.InfobloxHTTPPoolConnectionsKey, strconv.Itoa(config.Infoblox.HTTPPoolConnections))
	_ = os.Setenv(depresolver.OverrideWithFakeDNSKey, strconv.FormatBool(config.Override.FakeDNSEnabled))
	_ = os.Setenv(depresolver.OverrideFakeInfobloxKey, strconv.FormatBool(config.Override.FakeInfobloxEnabled))
	_ = os.Setenv(depresolver.LogLevelKey, config.Log.Level.String())
	_ = os.Setenv(depresolver.LogFormatKey, config.Log.Format.String())
	_ = os.Setenv(depresolver.LogNoColorKey, strconv.FormatBool(config.Log.NoColor))
}
