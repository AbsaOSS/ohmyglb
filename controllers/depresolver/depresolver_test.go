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
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"testing"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"github.com/AbsaOSS/k8gb/controllers/internal/utils"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

var predefinedConfig = Config{
	ReconcileRequeueSeconds: 30,
	ClusterGeoTag:           "us",
	ExtClustersGeoTags:      []string{"uk", "eu"},
	EdgeDNSType:             DNSTypeInfoblox,
	EdgeDNSServer:           "cloud.example.com",
	EdgeDNSZone:             "8.8.8.8",
	DNSZone:                 "example.com",
	K8gbNamespace:           "k8gb",
	Infoblox: Infoblox{
		"Infoblox.host.com",
		"0.0.3",
		443,
		"Infoblox",
		"secret",
		21,
		11,
	},
	Override: Override{
		false,
		false,
	},
	Log: Log{
		Format: JSONFormat,
	},
}

func TestResolveSpecWithFilledFields(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/filled_omitempty.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, cl)
	// assert
	assert.NoError(t, err)
	assert.Equal(t, 35, gslb.Spec.Strategy.DNSTtlSeconds)
	assert.Equal(t, 305, gslb.Spec.Strategy.SplitBrainThresholdSeconds)
}

func TestResolveSpecWithoutFields(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/free_omitempty.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, cl)
	// assert
	assert.NoError(t, err)
	assert.Equal(t, predefinedStrategy.DNSTtlSeconds, gslb.Spec.Strategy.DNSTtlSeconds)
	assert.Equal(t, predefinedStrategy.SplitBrainThresholdSeconds, gslb.Spec.Strategy.SplitBrainThresholdSeconds)
}

func TestResolveSpecWithZeroSplitBrain(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/filled_omitempty_with_zero_splitbrain.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, cl)
	// assert
	assert.NoError(t, err)
	assert.Equal(t, 35, gslb.Spec.Strategy.DNSTtlSeconds)
	assert.Equal(t, predefinedStrategy.SplitBrainThresholdSeconds, gslb.Spec.Strategy.SplitBrainThresholdSeconds)
}

func TestResolveSpecWithEmptyFields(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/invalid_omitempty_empty.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, cl)
	// assert
	assert.NoError(t, err)
	assert.Equal(t, predefinedStrategy.DNSTtlSeconds, gslb.Spec.Strategy.DNSTtlSeconds)
	assert.Equal(t, predefinedStrategy.SplitBrainThresholdSeconds, gslb.Spec.Strategy.SplitBrainThresholdSeconds)
}

func TestResolveSpecWithNegativeFields(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/invalid_omitempty_negative.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, cl)
	// assert
	assert.Error(t, err)
}

func TestSpecRunWhenChanged(t *testing.T) {
	// arrange
	cl, gslb := getTestContext("./testdata/filled_omitempty.yaml")
	ctx := context.Background()
	resolver := NewDependencyResolver()
	// act
	err1 := resolver.ResolveGslbSpec(ctx, gslb, cl)
	gslb.Spec.Strategy.SplitBrainThresholdSeconds = 0
	err2 := resolver.ResolveGslbSpec(ctx, gslb, cl)
	// assert
	assert.NoError(t, err1)
	// err2 would not be empty
	assert.NoError(t, err2)
	assert.Equal(t, predefinedStrategy.SplitBrainThresholdSeconds, gslb.Spec.Strategy.SplitBrainThresholdSeconds)
	assert.Equal(t, 35, gslb.Spec.Strategy.DNSTtlSeconds)
}

func TestResolveSpecWithNilClient(t *testing.T) {
	// arrange
	_, gslb := getTestContext("./testdata/filled_omitempty.yaml")
	resolver := NewDependencyResolver()
	// act
	err := resolver.ResolveGslbSpec(context.TODO(), gslb, nil)
	// assert
	assert.Error(t, err)
}

func TestResolveConfigWithMultipleInvalidEnv(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSZone = ""
	expected.EdgeDNSServer = ""
	expected.ExtClustersGeoTags = []string{}
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithoutEnvVarsSet(t *testing.T) {
	// arrange
	defer cleanup()
	defaultConfig := Config{}
	defaultConfig.ReconcileRequeueSeconds = 30
	defaultConfig.Infoblox.HTTPRequestTimeout = 20
	defaultConfig.Infoblox.HTTPPoolConnections = 10
	defaultConfig.EdgeDNSType = DNSTypeNoEdgeDNS
	defaultConfig.ExtClustersGeoTags = []string{}
	defaultConfig.Log.Level = zerolog.InfoLevel
	defaultConfig.Log.Format = JSONFormat
	defaultConfig.Log.NoColor = true
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.Error(t, err)
	assert.Equal(t, defaultConfig, *config)
}

func TestResolveConfigWithReconcileRequeueSecondsSync(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ReconcileRequeueSeconds = 3
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithTextReconcileRequeueSecondsSync(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(ReconcileRequeueSecondsKey, "invalid")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, predefinedConfig, *config)
}

func TestResolveConfigWithEmptyReconcileRequeueSecondsSync(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(ReconcileRequeueSecondsKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, predefinedConfig, *config)
}

func TestResolveConfigWithNegativeReconcileRequeueSecondsKey(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ReconcileRequeueSeconds = -1
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithZeroReconcileRequeueSecondsKey(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ReconcileRequeueSeconds = 0
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithEmptyReconcileRequeueSecondsKey(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(ReconcileRequeueSecondsKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, predefinedConfig, *config)
}

func TestResolveConfigWithoutReconcileRequeueSecondsKey(t *testing.T) {
	// arrange
	defer cleanup()
	// act,assert
	arrangeVariablesAndAssert(t, predefinedConfig, assert.NoError, ReconcileRequeueSecondsKey)
}

func TestResolveConfigWithMalformedGeoTag(t *testing.T) {
	// arrange
	defer cleanup()
	for _, tag := range []string{"eu-west.1", "eu?", " ", "eu west1", "?/"} {
		expected := predefinedConfig
		expected.ClusterGeoTag = tag
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.Error)
	}
}

func TestResolveConfigWithProperGeoTag(t *testing.T) {
	// arrange
	defer cleanup()
	for _, tag := range []string{"eu-west-1", "eu-west1", "us", "1", "US"} {
		expected := predefinedConfig
		expected.ClusterGeoTag = tag
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.NoError)
	}
}

func TestConfigRunOnce(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	resolver := NewDependencyResolver()
	// act
	config1, err1 := resolver.ResolveOperatorConfig()
	_ = os.Setenv(ReconcileRequeueSecondsKey, "100")
	// resolve again with new values
	config2, err2 := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err1)
	assert.Equal(t, predefinedConfig, *config1)
	// config2, err2 would be equal
	assert.NoError(t, err2)
	assert.Equal(t, *config1, *config2)
}

func TestResolveConfigWithMalformedRoute53Enabled(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(Route53EnabledKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.route53Enabled)
}

func TestResolveConfigWithProperRoute53Enabled(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.route53Enabled = true
	expected.Infoblox.Host = ""
	expected.EdgeDNSType = DNSTypeRoute53
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithoutRoute53(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.route53Enabled = false
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, Route53EnabledKey)
}

func TestResolveConfigWithEmptyRoute53(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(Route53EnabledKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.route53Enabled)
}

func TestResolveConfigWithProperNS1Enabled(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ns1Enabled = true
	expected.Infoblox.Host = ""
	expected.EdgeDNSType = DNSTypeNS1
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithoutNS1(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ns1Enabled = false
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, NS1EnabledKey)
}

func TestResolveConfigWithEmptyNS1(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(NS1EnabledKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.ns1Enabled)
}

func TestResolveConfigWithProperCoreDNSExposed(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.CoreDNSExposed = true
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithoutCoreDNSExposed(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.CoreDNSExposed = false
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, CoreDNSExposedKey)
}

func TestResolveConfigWithEmptyCoreDNSExposed(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(CoreDNSExposedKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.CoreDNSExposed)
}

func TestResolveConfigWithEmptyEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithNoEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, EdgeDNSServerKey)
}

func TestResolveConfigWithEmptyIpAddressInEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = "22.147.90.2"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithHostnameEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = "server-nonprod.on.domain.l3.2l.com"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)

}

func TestResolveConfigWithInvalidHostnameEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = "https://server-nonprod.on.domain.l3.2l.com"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithInvalidIpAddressEdgeDnsServer(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSServer = "22.147.90.2."
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithEmptyEdgeDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSZone = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithoutEdgeDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSZone = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, EdgeDNSZoneKey)
}

func TestResolveConfigWithHostnameEdgeDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSZone = "company.2l.com"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithInvalidHostnameEdgeDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSZone = "https://zone.com"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithValidHostnameDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.DNSZone = "3l2.zo-ne.com"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigWithEmptyHostnameDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.DNSZone = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithInvalidHostnameDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.DNSZone = "dns-zo?ne"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveConfigWithoutDnsZone(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.DNSZone = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, DNSZoneKey)
}

func TestResolveConfigWithEmptyK8gbNamespace(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.K8gbNamespace = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, K8gbNamespaceKey)
}

func TestResolveConfigWithInvalidK8gbNamespace(t *testing.T) {
	// arrange
	defer cleanup()
	for _, ns := range []string{"-", "Op.", "kube/netes", "my-ns???", "123-MY", "MY-123"} {
		expected := predefinedConfig
		expected.K8gbNamespace = ns
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.Error)
	}
}

func TestResolveConfigWithValidK8gbNamespace(t *testing.T) {
	// arrange
	defer cleanup()
	for _, ns := range []string{"k8gb", "my-123", "123-my", "n"} {
		expected := predefinedConfig
		expected.K8gbNamespace = ns
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.NoError)
	}
}

func TestResolveEmptyExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.DNSZone = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, DNSZoneKey)
}

func TestResolveOneExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ExtClustersGeoTags = []string{"foo"}
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveMultipleExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ExtClustersGeoTags = []string{"foo", "blah", "boom"}
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveUnsetExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ExtClustersGeoTags = []string{}
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, ExtClustersGeoTagsKey)
}

func TestResolveInvalidExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	for _, arr := range [][]string{{"good-tag", ".wrong.tag?"}, {"", ""}} {
		expected := predefinedConfig
		expected.ExtClustersGeoTags = arr
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.Error)
	}
}

func TestResolveGeoTagExistsWithinExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	tag := "us-west1"
	expected := predefinedConfig
	expected.ClusterGeoTag = tag
	expected.ExtClustersGeoTags = []string{"us-east1", tag}
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestResolveGeoTagWithRepeatingExtGeoTags(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.ExtClustersGeoTags = []string{"foo", "blah", "foo"}
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestRoute53IsEnabledAndInfobloxIsConfigured(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.route53Enabled = true
	expected.EdgeDNSType = DNSTypeRoute53 | DNSTypeInfoblox
	expected.Infoblox.Host = "Infoblox.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestRoute53IsDisabledAndInfobloxIsNotConfigured(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeNoEdgeDNS
	expected.route53Enabled = false
	expected.Infoblox.Host = ""
	// act,assert
	// that's how our integration tests are running
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestRoute53IsDisabledButInfobloxIsConfigured(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.route53Enabled = false
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "Infoblox.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestRoute53IsEnabledButInfobloxIsNotConfigured(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.route53Enabled = true
	expected.EdgeDNSType = DNSTypeRoute53
	expected.Infoblox.Host = ""
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestInfobloxGridHostIsEmpty(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeRoute53
	expected.route53Enabled = true
	expected.Infoblox.Host = ""
	expected.Infoblox.Version = ""
	expected.Infoblox.Port = 0
	expected.Infoblox.Username = ""
	expected.Infoblox.Password = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestInfobloxGridHostIsNotEmpty(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestInfobloxGridHostIsNotEmptyButInfobloxPropsAreEmpty(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = ""
	expected.Infoblox.Port = 0
	expected.Infoblox.Username = ""
	expected.Infoblox.Password = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestInfobloxGridHostIsEmptyButInfobloxPropsAreFilled(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeRoute53
	expected.route53Enabled = true
	expected.Infoblox.Host = ""
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestInfobloxGridHostIsUnset(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeNoEdgeDNS
	expected.route53Enabled = false
	expected.Infoblox.Host = ""
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	// values are ignored and not validated
	arrangeVariablesAndAssert(t, expected, assert.NoError, InfobloxGridHostKey)
}

func TestInfobloxGridHostIsInvalid(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.route53Enabled = false
	expected.Infoblox.Host = "dnfkjdnf kj"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestInfobloxVersionIsValid(t *testing.T) {
	// arrange
	defer cleanup()
	// version can be empty!
	for _, v := range []string{"0.0.1", "v0.0.1", "v0.0.0-patch1", "2.3.5-patch1"} {
		expected := predefinedConfig
		expected.EdgeDNSType = DNSTypeInfoblox
		expected.Infoblox.Host = "test.domain"
		expected.Infoblox.Version = v
		expected.Infoblox.Port = 443
		expected.Infoblox.Username = "foo"
		expected.Infoblox.Password = "blah"
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.NoError)
	}
}

func TestInfobloxVersionIsInvalid(t *testing.T) {
	// arrange
	defer cleanup()
	for _, v := range []string{"0.1.*", "kkojo", "k12k", ""} {
		expected := predefinedConfig
		expected.EdgeDNSType = DNSTypeInfoblox
		expected.Infoblox.Host = "test.domain"
		expected.Infoblox.Version = v
		expected.Infoblox.Port = 443
		expected.Infoblox.Username = "foo"
		expected.Infoblox.Password = "blah"
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.Error)
	}
}

func TestInfobloxVersionIsUnset(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = ""
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, InfobloxVersionKey)
}

func TestInvalidInfobloxPort(t *testing.T) {
	// arrange
	defer cleanup()
	for _, p := range []int{-1, 0, 65536} {
		expected := predefinedConfig
		expected.EdgeDNSType = DNSTypeInfoblox
		expected.Infoblox.Host = "test.domain"
		expected.Infoblox.Version = "0.0.1"
		expected.Infoblox.Port = p
		expected.Infoblox.Username = "foo"
		expected.Infoblox.Password = "blah"
		// act,assert
		arrangeVariablesAndAssert(t, expected, assert.Error)
	}
}

func TestUnsetInfobloxPort(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 0
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, InfobloxPortKey)
}

func TestValidInfobloxUserPasswordAndPort(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "infobloxUser"
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestEmptyInfobloxUser(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = ""
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestUnsetInfobloxUser(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = ""
	expected.Infoblox.Password = "blah"
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, InfobloxUsernameKey)
}

func TestEmptyInfobloxPassword(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "infobloxUser"
	expected.Infoblox.Password = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestUnsetInfobloxPassword(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.EdgeDNSType = DNSTypeInfoblox
	expected.Infoblox.Host = "test.domain"
	expected.Infoblox.Version = "0.0.1"
	expected.Infoblox.Port = 443
	expected.Infoblox.Username = "foo"
	expected.Infoblox.Password = ""
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error, InfobloxPasswordKey)
}

func TestValidInfobloxHTTPPoolConnections(t *testing.T) {
	// arrange
	defer cleanup()
	// act,assert
	arrangeVariablesAndAssert(t, predefinedConfig, assert.NoError)
}

func TestInvalidInfobloxHTTPPoolConnections(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(InfobloxHTTPPoolConnectionsKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, 10, config.Infoblox.HTTPPoolConnections)
}

func TestUnsetInfobloxHTTPPoolConnections(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPPoolConnections = 10
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, InfobloxHTTPPoolConnectionsKey)
}

func TestZeroInfobloxHTTPPoolConnections(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPPoolConnections = 0
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)

}

func TestNegativeInfobloxHTTPPoolConnections(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPPoolConnections = -1
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)

}

func TestValidInfobloxHTTPRequestTimeout(t *testing.T) {
	// arrange
	defer cleanup()
	// act,assert
	arrangeVariablesAndAssert(t, predefinedConfig, assert.NoError)
}

func TestInvalidInfobloxHTTPRequestTimeout(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(InfobloxHTTPRequestTimeoutKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, 20, config.Infoblox.HTTPRequestTimeout)
}

func TestUnsetInfobloxHTTPRequestTimeout(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPRequestTimeout = 20
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, InfobloxHTTPRequestTimeoutKey)

}

func TestZeroInfobloxHTTPRequestTimeout(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPRequestTimeout = 0
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)
}

func TestNegativeInfobloxHTTPRequestTimeout(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Infoblox.HTTPRequestTimeout = -1
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.Error)

}

func TestResolveConfigEnableFakeDNSAsTrue(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Override.FakeInfobloxEnabled = true
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigEnableFakeDNSAsFalse(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Override.FakeInfobloxEnabled = false
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigEnableFakeDNSAsInvalidValue(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(OverrideWithFakeDNSKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.Override.FakeDNSEnabled)
}

func TestResolveConfigEnableFakeDNSAsUnsetEnvironmentVariable(t *testing.T) {
	// arrange
	defer cleanup()
	// act,assert
	arrangeVariablesAndAssert(t, predefinedConfig, assert.NoError, OverrideWithFakeDNSKey)
}

func TestResolveConfigEnableFakeInfobloxAsTrue(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Override.FakeInfobloxEnabled = true
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveConfigEnableFakeInfobloxAsFalse(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Override.FakeInfobloxEnabled = false
	// act,assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)

}

func TestResolveConfigEnableFakeInfobloxAsInvalidValue(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(OverrideFakeInfobloxKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, false, config.Override.FakeInfobloxEnabled)
}

func TestResolveConfigEnableFakeInfobloxAsUnsetEnvironmentVariable(t *testing.T) {
	// arrange
	defer cleanup()
	// act,assert
	arrangeVariablesAndAssert(t, predefinedConfig, assert.NoError, OverrideFakeInfobloxKey)
}

func TestResolveLoggerUseDefaultValue(t *testing.T) {
	// arrange
	// Build zerolog from empty string which is equal to zerolog.NoLevel.
	// Depresolver handles it "" and use default value - info level
	defer cleanup()
	expected := predefinedConfig
	expected.Log.Level = zerolog.InfoLevel
	expected.Log.NoColor = true
	// act
	// assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, LogLevelKey, LogFormatKey, LogNoColorKey)
}

func TestResolveLoggerOutputFormatMode(t *testing.T) {
	// arrange
	defer cleanup()
	expected := predefinedConfig
	expected.Log.Format = SimpleFormat
	expected.Log.Level = zerolog.InfoLevel
	// act
	// assert
	arrangeVariablesAndAssert(t, expected, assert.NoError, LogLevelKey)
}

func TestResolveLoggerDebugMode(t *testing.T) {
	// arrange
	expected := predefinedConfig
	expected.Log.Level = zerolog.DebugLevel
	// act
	// assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveLoggerNoColor(t *testing.T) {
	// arrange
	expected := predefinedConfig
	expected.Log.NoColor = true
	// act
	// assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveLoggerInfoMode(t *testing.T) {
	// arrange
	expected := predefinedConfig
	expected.Log.Level = zerolog.InfoLevel
	// act
	// assert
	arrangeVariablesAndAssert(t, expected, assert.NoError)
}

func TestResolveLoggerCaseInsensitiveMode(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogLevelKey, "WARn")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, zerolog.WarnLevel, config.Log.Level)
}

func TestResolveLoggerCaseInsensitiveOutputFormat(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogFormatKey, "Json")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, JSONFormat, config.Log.Format)
}

func TestResolveLoggerLevelWithInvalidValue(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogLevelKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.Error(t, err)
	assert.Equal(t, zerolog.NoLevel, config.Log.Level)
	assert.Equal(t, JSONFormat, config.Log.Format)
}

func TestResolveLoggerNoColorInvalidValue(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogNoColorKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, true, config.Log.NoColor)
}

func TestResolveLoggerOutputWithInvalidValue(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogFormatKey, "i.am.wrong??.")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.Error(t, err)
	assert.Equal(t, NoFormat, config.Log.Format)
}

func TestResolveLoggerWithEmptyValues(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogFormatKey, "")
	_ = os.Setenv(LogLevelKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, JSONFormat, config.Log.Format)
	assert.Equal(t, zerolog.InfoLevel, config.Log.Level)
}

func TestResolveLoggerEmptyValues(t *testing.T) {
	// arrange
	defer cleanup()
	configureEnvVar(predefinedConfig)
	_ = os.Setenv(LogFormatKey, "")
	_ = os.Setenv(LogLevelKey, "")
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	assert.NoError(t, err)
	assert.Equal(t, zerolog.InfoLevel, config.Log.Level)
	assert.Equal(t, JSONFormat, config.Log.Format)
}

// arrangeVariablesAndAssert sets string environment variables and asserts `expected` argument with
// ResolveOperatorConfig() output. The last parameter unsets the values
func arrangeVariablesAndAssert(t *testing.T, expected Config,
	errf func(t assert.TestingT, err error, msgAndArgs ...interface{}) bool, unset ...string) {
	configureEnvVar(expected)
	for _, v := range unset {
		_ = os.Unsetenv(v)
	}
	resolver := NewDependencyResolver()
	// act
	config, err := resolver.ResolveOperatorConfig()
	// assert
	if config == nil {
		t.Fatal("nil *config returned")
	}
	assert.Equal(t, expected, *config)
	errf(t, err)
}

func cleanup() {
	for _, s := range []string{ReconcileRequeueSecondsKey, ClusterGeoTagKey, ExtClustersGeoTagsKey, EdgeDNSZoneKey, DNSZoneKey, EdgeDNSServerKey,
		Route53EnabledKey, NS1EnabledKey, InfobloxGridHostKey, InfobloxVersionKey, InfobloxPortKey, InfobloxUsernameKey, InfobloxPasswordKey,
		OverrideWithFakeDNSKey, OverrideFakeInfobloxKey, K8gbNamespaceKey, CoreDNSExposedKey, InfobloxHTTPRequestTimeoutKey,
		InfobloxHTTPPoolConnectionsKey, LogLevelKey, LogFormatKey, LogNoColorKey} {
		if os.Unsetenv(s) != nil {
			panic(fmt.Errorf("cleanup %s", s))
		}
	}
}

func configureEnvVar(config Config) {
	_ = os.Setenv(ReconcileRequeueSecondsKey, strconv.Itoa(config.ReconcileRequeueSeconds))
	_ = os.Setenv(ClusterGeoTagKey, config.ClusterGeoTag)
	_ = os.Setenv(ExtClustersGeoTagsKey, strings.Join(config.ExtClustersGeoTags, ","))
	_ = os.Setenv(EdgeDNSServerKey, config.EdgeDNSServer)
	_ = os.Setenv(EdgeDNSZoneKey, config.EdgeDNSZone)
	_ = os.Setenv(DNSZoneKey, config.DNSZone)
	_ = os.Setenv(K8gbNamespaceKey, config.K8gbNamespace)
	_ = os.Setenv(Route53EnabledKey, strconv.FormatBool(config.route53Enabled))
	_ = os.Setenv(NS1EnabledKey, strconv.FormatBool(config.ns1Enabled))
	_ = os.Setenv(CoreDNSExposedKey, strconv.FormatBool(config.CoreDNSExposed))
	_ = os.Setenv(InfobloxGridHostKey, config.Infoblox.Host)
	_ = os.Setenv(InfobloxVersionKey, config.Infoblox.Version)
	_ = os.Setenv(InfobloxPortKey, strconv.Itoa(config.Infoblox.Port))
	_ = os.Setenv(InfobloxUsernameKey, config.Infoblox.Username)
	_ = os.Setenv(InfobloxPasswordKey, config.Infoblox.Password)
	_ = os.Setenv(InfobloxHTTPRequestTimeoutKey, strconv.Itoa(config.Infoblox.HTTPRequestTimeout))
	_ = os.Setenv(InfobloxHTTPPoolConnectionsKey, strconv.Itoa(config.Infoblox.HTTPPoolConnections))
	_ = os.Setenv(OverrideWithFakeDNSKey, strconv.FormatBool(config.Override.FakeDNSEnabled))
	_ = os.Setenv(OverrideFakeInfobloxKey, strconv.FormatBool(config.Override.FakeInfobloxEnabled))
	_ = os.Setenv(LogLevelKey, config.Log.Level.String())
	_ = os.Setenv(LogFormatKey, config.Log.Format.String())
	_ = os.Setenv(LogNoColorKey, strconv.FormatBool(config.Log.NoColor))

}

func getTestContext(testData string) (client.Client, *k8gbv1beta1.Gslb) {
	// Create a fake client to mock API calls.
	var gslbYaml, err = ioutil.ReadFile(testData)
	if err != nil {
		panic(fmt.Errorf("can't open example CR file: %s", testData))
	}
	gslb, err := utils.YamlToGslb(gslbYaml)
	if err != nil {
		panic(err)
	}
	objs := []runtime.Object{
		gslb,
	}
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(k8gbv1beta1.GroupVersion, gslb)
	// Register external-dns DNSEndpoint CRD
	s.AddKnownTypes(schema.GroupVersion{Group: "externaldns.k8s.io", Version: "v1alpha1"}, &externaldns.DNSEndpoint{})
	cl := fake.NewFakeClientWithScheme(s, objs...)
	return cl, gslb
}
