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

package assistant

import (
	"context"
	coreerrors "errors"
	"fmt"
	"strings"
	"time"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"github.com/AbsaOSS/k8gb/controllers/internal/utils"
	"github.com/AbsaOSS/k8gb/controllers/log"

	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

const coreDNSExtServiceName = "k8gb-coredns-lb"

// GslbLoggerAssistant is common wrapper operating on GSLB instance.
// It uses apimachinery client to call kubernetes API
type GslbLoggerAssistant struct {
	client        client.Client
	k8gbNamespace string
	edgeDNSServer string
}

var logger = log.Logger()

func NewGslbAssistant(client client.Client, k8gbNamespace, edgeDNSServer string) *GslbLoggerAssistant {
	return &GslbLoggerAssistant{
		client:        client,
		k8gbNamespace: k8gbNamespace,
		edgeDNSServer: edgeDNSServer,
	}
}

// CoreDNSExposedIPs retrieves list of IP's exposed by CoreDNS
func (r *GslbLoggerAssistant) CoreDNSExposedIPs() ([]string, error) {
	coreDNSService := &corev1.Service{}
	err := r.client.Get(context.TODO(),
		types.NamespacedName{Namespace: r.k8gbNamespace, Name: coreDNSExtServiceName}, coreDNSService)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Warn().Msgf("Can't find %s service", coreDNSExtServiceName)
		}
		return nil, err
	}
	var lbHostname string
	if len(coreDNSService.Status.LoadBalancer.Ingress) > 0 {
		lbHostname = coreDNSService.Status.LoadBalancer.Ingress[0].Hostname
	} else {
		errMessage := fmt.Sprintf("no Ingress LoadBalancer entries found for %s serice", coreDNSExtServiceName)
		logger.Warn().Msg(errMessage)
		err := coreerrors.New(errMessage)
		return nil, err
	}
	IPs, err := utils.Dig(r.edgeDNSServer, lbHostname)
	if err != nil {
		logger.Warn().Msgf("Can't dig k8gb-coredns-lb service loadbalancer fqdn %s (%s)", lbHostname, err)
		return nil, err
	}
	return IPs, nil
}

// GslbIngressExposedIPs retrieves list of IP's exposed by all GSLB ingresses
func (r *GslbLoggerAssistant) GslbIngressExposedIPs(gslb *k8gbv1beta1.Gslb) ([]string, error) {
	nn := types.NamespacedName{
		Name:      gslb.Name,
		Namespace: gslb.Namespace,
	}

	gslbIngress := &v1beta1.Ingress{}

	err := r.client.Get(context.TODO(), nn, gslbIngress)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info().Msgf("Can't find gslb Ingress: %s", gslb.Name)
		}
		return nil, err
	}

	var gslbIngressIPs []string

	for _, ip := range gslbIngress.Status.LoadBalancer.Ingress {
		if len(ip.IP) > 0 {
			gslbIngressIPs = append(gslbIngressIPs, ip.IP)
		}
		if len(ip.Hostname) > 0 {
			IPs, err := utils.Dig(r.edgeDNSServer, ip.Hostname)
			if err != nil {
				logger.Warn().Msgf("Dig error: %s", err)
				return nil, err
			}
			gslbIngressIPs = append(gslbIngressIPs, IPs...)
		}
	}

	return gslbIngressIPs, nil
}

// SaveDNSEndpoint update DNS endpoint or create new one if doesnt exist
func (r *GslbLoggerAssistant) SaveDNSEndpoint(namespace string, i *externaldns.DNSEndpoint) error {
	found := &externaldns.DNSEndpoint{}
	err := r.client.Get(context.TODO(), types.NamespacedName{
		Name:      i.Name,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		// Create the DNSEndpoint
		logger.Info().Msgf("Creating a new DNSEndpoint:\n %s", utils.ToString(i))
		err = r.client.Create(context.TODO(), i)

		if err != nil {
			// Creation failed
			logger.Err(err).Msgf("Failed to create new DNSEndpoint DNSEndpoint.Namespace: %s DNSEndpoint.Name %s",
				i.Namespace, i.Name)
			return err
		}
		// Creation was successful
		return nil
	} else if err != nil {
		// Error that isn't due to the service not existing
		logger.Err(err).Msg("Failed to get DNSEndpoint")
		return err
	}

	// Update existing object with new spec
	found.Spec = i.Spec
	err = r.client.Update(context.TODO(), found)

	if err != nil {
		// Update failed
		logger.Err(err).Msgf("Failed to update DNSEndpoint DNSEndpoint.Namespace %s DNSEndpoint.Name %s",
			found.Namespace, found.Name)
		return err
	}
	return nil
}

// RemoveEndpoint removes endpoint
func (r *GslbLoggerAssistant) RemoveEndpoint(endpointName string) error {
	logger.Info().Msgf("Removing endpoint %s.%s", r.k8gbNamespace, endpointName)
	dnsEndpoint := &externaldns.DNSEndpoint{}
	err := r.client.Get(context.Background(), client.ObjectKey{Namespace: r.k8gbNamespace, Name: endpointName}, dnsEndpoint)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Warn().Msgf("%s", err)
			return nil
		}
		return err
	}
	err = r.client.Delete(context.TODO(), dnsEndpoint)
	return err
}

// InspectTXTThreshold inspects fqdn TXT record from edgeDNSServer. If record doesn't exists or timestamp is greater than
// splitBrainThreshold the error is returned. In case fakeDNSEnabled is true, 127.0.0.1:7753 is used as edgeDNSServer
func (r *GslbLoggerAssistant) InspectTXTThreshold(fqdn string, fakeDNSEnabled bool, splitBrainThreshold time.Duration) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(fqdn), dns.TypeTXT)
	ns := overrideWithFakeDNS(fakeDNSEnabled, r.edgeDNSServer)
	txt, err := dns.Exchange(m, ns)
	if err != nil {
		logger.Info().Msgf("Error contacting EdgeDNS server (%s) for TXT split brain record: (%s)", ns, err)
		return err
	}
	var timestamp string
	if len(txt.Answer) > 0 {
		if t, ok := txt.Answer[0].(*dns.TXT); ok {
			logger.Info().Msgf("Split brain TXT raw record: %s", t.String())
			timestamp = strings.Split(t.String(), "\t")[4]
			timestamp = strings.Trim(timestamp, "\"") // Otherwise time.Parse() will miserably fail
		}
	}

	if len(timestamp) > 0 {
		logger.Info().Msgf("Split brain TXT raw time stamp: %s", timestamp)
		timeFromTXT, err := time.Parse("2006-01-02T15:04:05", timestamp)
		if err != nil {
			return err
		}

		logger.Info().Msgf("Split brain TXT parsed time stamp: %s", timeFromTXT)
		now := time.Now().UTC()

		diff := now.Sub(timeFromTXT)
		logger.Info().Msgf("Split brain TXT time diff: %s", diff)

		if diff > splitBrainThreshold {
			return errors.NewResourceExpired(fmt.Sprintf("Split brain TXT record expired the time threshold: (%s)", splitBrainThreshold))
		}
		return nil
	}
	return errors.NewResourceExpired(fmt.Sprintf("Can't find split brain TXT record at EdgeDNS server(%s) and record %s ", ns, fqdn))
}

func (r *GslbLoggerAssistant) GetExternalTargets(host string, fakeDNSEnabled bool, extGslbClusters []string) (targets []string) {
	targets = []string{}
	for _, cluster := range extGslbClusters {
		logger.Info().Msgf("Adding external Gslb targets from %s cluster...", cluster)
		g := new(dns.Msg)
		host = fmt.Sprintf("localtargets-%s.", host) // Convert to true FQDN with dot at the end. Otherwise dns lib freaks out
		g.SetQuestion(host, dns.TypeA)

		ns := overrideWithFakeDNS(fakeDNSEnabled, cluster)

		a, err := dns.Exchange(g, ns)
		if err != nil {
			logger.Warn().Msgf("Contacting external Gslb cluster(%s) : (%v)", cluster, err)
			return
		}
		var clusterTargets []string

		for _, A := range a.Answer {
			IP := strings.Split(A.String(), "\t")[4]
			clusterTargets = append(clusterTargets, IP)
		}
		if len(clusterTargets) > 0 {
			targets = append(targets, clusterTargets...)
			logger.Info().Msgf("Added external %s Gslb targets from %s cluster", clusterTargets, cluster)
		}
	}
	return
}

func overrideWithFakeDNS(fakeDNSEnabled bool, server string) (ns string) {
	if fakeDNSEnabled {
		ns = "127.0.0.1:7753"
	} else {
		ns = fmt.Sprintf("%s:53", server)
	}
	return
}
