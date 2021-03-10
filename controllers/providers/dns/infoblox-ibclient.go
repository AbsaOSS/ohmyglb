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
	"strconv"

	ibclient "github.com/infobloxopen/infoblox-go-client"
)

func (p *InfobloxProvider) infobloxConnection() (*ibclient.ObjectManager, error) {
	hostConfig := ibclient.HostConfig{
		Host:     p.config.Infoblox.Host,
		Version:  p.config.Infoblox.Version,
		Port:     strconv.Itoa(p.config.Infoblox.Port),
		Username: p.config.Infoblox.Username,
		Password: p.config.Infoblox.Password,
	}
	transportConfig := ibclient.NewTransportConfig("false", p.config.Infoblox.HTTPRequestTimeout, p.config.Infoblox.HTTPPoolConnections)
	requestBuilder := &ibclient.WapiRequestBuilder{}
	requestor := &ibclient.WapiHttpRequestor{}

	var objMgr *ibclient.ObjectManager

	if p.config.Override.FakeInfobloxEnabled {
		fqdn := "fakezone.example.com"
		fakeRefReturn := "zone_delegated/ZG5zLnpvbmUkLl9kZWZhdWx0LnphLmNvLmFic2EuY2Fhcy5vaG15Z2xiLmdzbGJpYmNsaWVudA:fakezone.example.com/default"
		ohmyFakeConnector := &fakeInfobloxConnector{
			getObjectObj: ibclient.NewZoneDelegated(ibclient.ZoneDelegated{Fqdn: fqdn}),
			getObjectRef: "",
			resultObject: []ibclient.ZoneDelegated{*ibclient.NewZoneDelegated(ibclient.ZoneDelegated{Fqdn: fqdn, Ref: fakeRefReturn})},
		}
		objMgr = ibclient.NewObjectManager(ohmyFakeConnector, "ohmyclient", "")
	} else {
		conn, err := ibclient.NewConnector(hostConfig, transportConfig, requestBuilder, requestor)
		if err != nil {
			return nil, err
		}
		defer func() {
			err = conn.Logout()
			if err != nil {
				logger.Err(err).Msg("Failed to close connection to infoblox")
			}
		}()
		objMgr = ibclient.NewObjectManager(conn, "ohmyclient", "")
	}
	return objMgr, nil
}

func (p *InfobloxProvider) checkZoneDelegated(findZone *ibclient.ZoneDelegated) error {
	if findZone.Fqdn != p.config.DNSZone {
		err := fmt.Errorf("delegated zone returned from infoblox(%s) does not match requested gslb zone(%s)", findZone.Fqdn, p.config.DNSZone)
		return err
	}
	return nil
}

func (p *InfobloxProvider) filterOutDelegateTo(delegateTo []ibclient.NameServer, fqdn string) []ibclient.NameServer {
	for i := 0; i < len(delegateTo); i++ {
		if delegateTo[i].Name == fqdn {
			delegateTo = append(delegateTo[:i], delegateTo[i+1:]...)
			i--
		}
	}
	return delegateTo
}
