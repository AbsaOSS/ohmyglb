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

// fake dns server that is used for external dns communication tests of k8gb

package controllers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/miekg/dns"
)

func oldEdgeTimestamp(threshold string) string {
	now := time.Now()
	duration, _ := time.ParseDuration(threshold)
	before := now.Add(-duration)
	edgeTimestamp := fmt.Sprint(before.UTC().Format("2006-01-02T15:04:05"))
	return edgeTimestamp
}

var records = map[string][]string{
	"localtargets-roundrobin.cloud.example.com.": {"10.1.0.3", "10.1.0.2", "10.1.0.1"},
	"test-gslb-heartbeat-eu.example.com.":        {oldEdgeTimestamp("10m")},
	"test-gslb-heartbeat-za.example.com.":        {oldEdgeTimestamp("3m")},
}

func parseQuery(m *dns.Msg) {
	for _, q := range m.Question {
		switch q.Qtype {
		case dns.TypeA:
			logger.Info().Msgf("Query for %s\n", q.Name)
			ips := records[q.Name]
			logger.Info().Msgf("IPs found: %s\n", ips)
			if len(ips) > 0 {
				for _, ip := range ips {
					rr, err := dns.NewRR(fmt.Sprintf("%s A %s", q.Name, ip))
					if err == nil {
						m.Answer = append(m.Answer, rr)
					}
				}
			}
		case dns.TypeTXT:
			logger.Info().Msgf("Query for TXT %s\n", q.Name)
			TXTs := records[q.Name]
			logger.Info().Msgf("TXTs found: %s\n", TXTs)
			if len(TXTs) > 0 {
				for _, txt := range TXTs {
					rr, err := dns.NewRR(fmt.Sprintf("%s TXT %s", q.Name, txt))
					if err == nil {
						m.Answer = append(m.Answer, rr)
					}
				}
			}
		}
	}
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	if r.Opcode == dns.OpcodeQuery {
		parseQuery(m)
	}

	err := w.WriteMsg(m)
	if err != nil {
		logger.Err(err).Msg("Failed to write message")
	}
}

func fakeDNS() {
	// attach request handler func
	dns.HandleFunc("example.com.", handleDNSRequest)

	// start server
	port := 7753
	server := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	go func() {
		logger.Info().Msgf("Starting at %d\n", port)
		err := server.ListenAndServe()
		defer func() {
			err := server.Shutdown()
			if err != nil {
				logger.Err(err).Msg("Failed to shutdown fakeDNS server")
			}

		}()
		if err != nil {
			logger.Err(err).Msg("Failed to start fakeDNS server")
		}
	}()
}
