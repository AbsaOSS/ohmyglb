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
	"fmt"
	"strings"

	"github.com/AbsaOSS/gopkg/env"
	"github.com/rs/zerolog"
)

// Environment variables keys
const (
	ReconcileRequeueSecondsKey = "RECONCILE_REQUEUE_SECONDS"
	ClusterGeoTagKey           = "CLUSTER_GEO_TAG"
	ExtClustersGeoTagsKey      = "EXT_GSLB_CLUSTERS_GEO_TAGS"
	Route53EnabledKey          = "ROUTE53_ENABLED"
	NS1EnabledKey              = "NS1_ENABLED"
	EdgeDNSServerKey           = "EDGE_DNS_SERVER"
	EdgeDNSZoneKey             = "EDGE_DNS_ZONE"
	DNSZoneKey                 = "DNS_ZONE"
	InfobloxGridHostKey        = "INFOBLOX_GRID_HOST"
	InfobloxVersionKey         = "INFOBLOX_WAPI_VERSION"
	InfobloxPortKey            = "INFOBLOX_WAPI_PORT"
	InfobloxUsernameKey        = "EXTERNAL_DNS_INFOBLOX_WAPI_USERNAME"
	// #nosec G101; ignore false positive gosec; see: https://securego.io/docs/rules/g101.html
	InfobloxPasswordKey            = "EXTERNAL_DNS_INFOBLOX_WAPI_PASSWORD"
	InfobloxHTTPRequestTimeoutKey  = "INFOBLOX_HTTP_REQUEST_TIMEOUT"
	InfobloxHTTPPoolConnectionsKey = "INFOBLOX_HTTP_POOL_CONNECTIONS"
	OverrideWithFakeDNSKey         = "OVERRIDE_WITH_FAKE_EXT_DNS"
	OverrideFakeInfobloxKey        = "FAKE_INFOBLOX"
	K8gbNamespaceKey               = "POD_NAMESPACE"
	CoreDNSExposedKey              = "COREDNS_EXPOSED"
	LoggerLevelKey                 = "LOGGER_LEVEL"
	LoggerOutputFormatKey          = "LOGGER_OUTPUT_FORMAT"
)

// ResolveOperatorConfig executes once. It reads operator's configuration
// from environment variables into &Config and validates
func (dr *DependencyResolver) ResolveOperatorConfig() (*Config, error) {
	dr.onceConfig.Do(func() {
		dr.config = &Config{}
		dr.config.ReconcileRequeueSeconds, _ = env.GetEnvAsIntOrFallback(ReconcileRequeueSecondsKey, 30)
		dr.config.ClusterGeoTag = env.GetEnvAsStringOrFallback(ClusterGeoTagKey, "")
		dr.config.ExtClustersGeoTags = env.GetEnvAsArrayOfStringsOrFallback(ExtClustersGeoTagsKey, []string{})
		dr.config.route53Enabled = env.GetEnvAsBoolOrFallback(Route53EnabledKey, false)
		dr.config.ns1Enabled = env.GetEnvAsBoolOrFallback(NS1EnabledKey, false)
		dr.config.CoreDNSExposed = env.GetEnvAsBoolOrFallback(CoreDNSExposedKey, false)
		dr.config.EdgeDNSServer = env.GetEnvAsStringOrFallback(EdgeDNSServerKey, "")
		dr.config.EdgeDNSZone = env.GetEnvAsStringOrFallback(EdgeDNSZoneKey, "")
		dr.config.DNSZone = env.GetEnvAsStringOrFallback(DNSZoneKey, "")
		dr.config.K8gbNamespace = env.GetEnvAsStringOrFallback(K8gbNamespaceKey, "")
		dr.config.Infoblox.Host = env.GetEnvAsStringOrFallback(InfobloxGridHostKey, "")
		dr.config.Infoblox.Version = env.GetEnvAsStringOrFallback(InfobloxVersionKey, "")
		dr.config.Infoblox.Port, _ = env.GetEnvAsIntOrFallback(InfobloxPortKey, 0)
		dr.config.Infoblox.Username = env.GetEnvAsStringOrFallback(InfobloxUsernameKey, "")
		dr.config.Infoblox.Password = env.GetEnvAsStringOrFallback(InfobloxPasswordKey, "")
		dr.config.Infoblox.HTTPPoolConnections, _ = env.GetEnvAsIntOrFallback(InfobloxHTTPPoolConnectionsKey, 10)
		dr.config.Infoblox.HTTPRequestTimeout, _ = env.GetEnvAsIntOrFallback(InfobloxHTTPRequestTimeoutKey, 20)
		dr.config.Override.FakeDNSEnabled = env.GetEnvAsBoolOrFallback(OverrideWithFakeDNSKey, false)
		dr.config.Override.FakeInfobloxEnabled = env.GetEnvAsBoolOrFallback(OverrideFakeInfobloxKey, false)
		dr.config.Logger.Level, _ = zerolog.ParseLevel(strings.ToLower(env.GetEnvAsStringOrFallback(LoggerLevelKey, zerolog.InfoLevel.String())))
		dr.config.Logger.OutputFormat = parseLoggerOutputFormat(strings.ToLower(env.GetEnvAsStringOrFallback(LoggerOutputFormatKey, "mono")))
		dr.errorConfig = dr.validateConfig(dr.config)
		dr.config.EdgeDNSType = getEdgeDNSType(dr.config)
	})
	return dr.config, dr.errorConfig
}

func (dr *DependencyResolver) validateConfig(config *Config) (err error) {
	if config.Logger.Level == zerolog.NoLevel {
		config.Logger.Level = zerolog.InfoLevel
		return fmt.Errorf("invalid %s, allowed values ['','trace','debug','info','warn','error','fatal','panic']", LoggerLevelKey)
	}
	if config.Logger.OutputFormat == unrecognisedFormat {
		config.Logger.OutputFormat = ConsoleMonoFormat
		return fmt.Errorf("invalid %s, allowed values ['','json','mono','color']", LoggerOutputFormatKey)
	}
	err = field("k8gbNamespace", config.K8gbNamespace).isNotEmpty().matchRegexp(k8sNamespaceRegex).err
	if err != nil {
		return err
	}
	err = field("reconcileRequeueSeconds", config.ReconcileRequeueSeconds).isHigherThanZero().err
	if err != nil {
		return err
	}
	err = field("clusterGeoTag", config.ClusterGeoTag).isNotEmpty().matchRegexp(geoTagRegex).err
	if err != nil {
		return err
	}
	err = field("extClusterGeoTags", config.ExtClustersGeoTags).hasItems().hasUniqueItems().err
	if err != nil {
		return err
	}
	for i, geoTag := range config.ExtClustersGeoTags {
		err = field(fmt.Sprintf("extClustersGeoTags[%v]", i), geoTag).isNotEmpty().matchRegexp(geoTagRegex).isNotEqualTo(config.ClusterGeoTag).err
		if err != nil {
			return err
		}
	}
	err = field("edgeDNSServer", config.EdgeDNSServer).isNotEmpty().matchRegexps(hostNameRegex, ipAddressRegex).err
	if err != nil {
		return err
	}
	err = field("edgeDNSZone", config.EdgeDNSZone).isNotEmpty().matchRegexp(hostNameRegex).err
	if err != nil {
		return err
	}
	err = field("DNSZone", config.DNSZone).isNotEmpty().matchRegexp(hostNameRegex).err
	if err != nil {
		return err
	}
	// do full Infoblox validation only in case that Host exists
	if isNotEmpty(config.Infoblox.Host) {
		err = field("InfobloxGridHost", config.Infoblox.Host).matchRegexps(hostNameRegex, ipAddressRegex).err
		if err != nil {
			return err
		}
		err = field("InfobloxVersion", config.Infoblox.Version).isNotEmpty().matchRegexp(versionNumberRegex).err
		if err != nil {
			return err
		}
		err = field("InfobloxPort", config.Infoblox.Port).isHigherThanZero().isLessOrEqualTo(65535).err
		if err != nil {
			return err
		}
		err = field("InfobloxUsername", config.Infoblox.Username).isNotEmpty().err
		if err != nil {
			return err
		}
		err = field("InfobloxPassword", config.Infoblox.Password).isNotEmpty().err
		if err != nil {
			return err
		}
		err = field("InfobloxHTTPPoolConnections", config.Infoblox.HTTPPoolConnections).isHigherOrEqualToZero().err
		if err != nil {
			return err
		}
		err = field("InfobloxHTTPRequestTimeout", config.Infoblox.HTTPRequestTimeout).isHigherThanZero().err
		if err != nil {
			return err
		}
	}
	return nil
}

// getEdgeDNSType contains logic retrieving EdgeDNSType
func getEdgeDNSType(config *Config) EdgeDNSType {
	var t = DNSTypeNoEdgeDNS
	if config.ns1Enabled {
		t |= DNSTypeNS1
	}
	if config.route53Enabled {
		t |= DNSTypeRoute53
	}
	if isNotEmpty(config.Infoblox.Host) {
		t |= DNSTypeInfoblox
	}
	if t > DNSTypeNoEdgeDNS {
		t -= DNSTypeNoEdgeDNS
	}
	return t
}

func parseLoggerOutputFormat(value string) LoggerOutputFormat {
	switch value {
	case "json":
		return JSONFormat
	case "mono":
		return ConsoleMonoFormat
	case "color":
		return ConsoleColoredFormat
	}
	return unrecognisedFormat
}
