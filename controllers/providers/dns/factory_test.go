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

package dns

import (
	"fmt"
	"testing"

	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/AbsaOSS/k8gb/controllers/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFactoryInfoblox(t *testing.T) {
	// arrange
	client := fake.NewFakeClientWithScheme(scheme.Scheme, []runtime.Object{}...)
	customConfig := predefinedConfig
	customConfig.EdgeDNSType = depresolver.DNSTypeInfoblox
	// act
	f, err := NewDNSProviderFactory(client, customConfig)
	require.NoError(t, err)
	provider := f.Provider()
	// assert
	assert.NotNil(t, provider)
	assert.Equal(t, "*InfobloxProvider", utils.GetType(provider))
	assert.Equal(t, "Infoblox", fmt.Sprintf("%s", provider))
}

func TestFactoryNS1(t *testing.T) {
	// arrange
	client := fake.NewFakeClientWithScheme(scheme.Scheme, []runtime.Object{}...)
	customConfig := predefinedConfig
	customConfig.EdgeDNSType = depresolver.DNSTypeNS1
	// act
	f, err := NewDNSProviderFactory(client, customConfig)
	require.NoError(t, err)
	provider := f.Provider()
	// assert
	assert.NotNil(t, provider)
	assert.Equal(t, "*ExternalDNSProvider", utils.GetType(provider))
	assert.Equal(t, "NS1", fmt.Sprintf("%s", provider))
}

func TestFactoryRoute53(t *testing.T) {
	// arrange
	client := fake.NewFakeClientWithScheme(scheme.Scheme, []runtime.Object{}...)
	customConfig := predefinedConfig
	customConfig.EdgeDNSType = depresolver.DNSTypeRoute53
	// act
	f, err := NewDNSProviderFactory(client, customConfig)
	require.NoError(t, err)
	provider := f.Provider()
	// assert
	assert.NotNil(t, provider)
	assert.Equal(t, "*ExternalDNSProvider", utils.GetType(provider))
	assert.Equal(t, "ROUTE53", fmt.Sprintf("%s", provider))
}

func TestFactoryNoEdgeDNS(t *testing.T) {
	// arrange
	client := fake.NewFakeClientWithScheme(scheme.Scheme, []runtime.Object{}...)
	customConfig := predefinedConfig
	customConfig.EdgeDNSType = depresolver.DNSTypeNoEdgeDNS
	// act
	f, err := NewDNSProviderFactory(client, customConfig)
	require.NoError(t, err)
	provider := f.Provider()
	// assert
	assert.Equal(t, "*EmptyDNSProvider", utils.GetType(provider))
	assert.Equal(t, "EMPTY", fmt.Sprintf("%s", provider))
}

func TestFactoryNilClient(t *testing.T) {
	// arrange
	customConfig := predefinedConfig
	customConfig.EdgeDNSType = depresolver.DNSTypeNoEdgeDNS
	// act
	// assert
	_, err := NewDNSProviderFactory(nil, customConfig)
	require.Error(t, err)
}
