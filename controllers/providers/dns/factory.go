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

	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/AbsaOSS/k8gb/controllers/providers/assistant"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProviderFactory struct {
	config depresolver.Config
	client client.Client
	log    logr.Logger
}

func NewDNSProviderFactory(client client.Client, config depresolver.Config, log logr.Logger) (f *ProviderFactory, err error) {
	if log == nil {
		err = fmt.Errorf("nil log")
	}
	if client == nil {
		err = fmt.Errorf("nil client")
	}
	f = &ProviderFactory{
		config: config,
		log:    log,
		client: client,
	}
	return
}

func (f *ProviderFactory) Provider() (provider IDnsProvider) {
	a := assistant.NewGslbAssistant(f.client, f.log, f.config.K8gbNamespace, f.config.EdgeDNSServer)
	switch f.config.EdgeDNSType {
	case depresolver.DNSTypeNS1:
		provider = NewExternalDNS(externalDNSTypeNS1, f.config, a)
	case depresolver.DNSTypeRoute53:
		provider = NewExternalDNS(externalDNSTypeRoute53, f.config, a)
	case depresolver.DNSTypeInfoblox:
		provider = NewInfobloxDNS(f.config, a)
	case depresolver.DNSTypeNoEdgeDNS:
		provider = NewEmptyDNS(f.config, a)
	}
	return
}
