/*
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

package main

import (
	"flag"
	"os"

	k8gbv1beta1 "github.com/AbsaOSS/k8gb/api/v1beta1"
	"github.com/AbsaOSS/k8gb/controllers"
	"github.com/AbsaOSS/k8gb/controllers/depresolver"
	"github.com/AbsaOSS/k8gb/controllers/log"
	"github.com/AbsaOSS/k8gb/controllers/providers/dns"
	"github.com/AbsaOSS/k8gb/controllers/providers/metrics"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	// +kubebuilder:scaffold:imports
)

var (
	runtimescheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(runtimescheme))

	utilruntime.Must(k8gbv1beta1.AddToScheme(runtimescheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var f *dns.ProviderFactory
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	resolver := depresolver.NewDependencyResolver()
	config, err := resolver.ResolveOperatorConfig()
	log.Init(config)
	logger := log.Logger()
	if err != nil {
		logger.Err(err).Msg("can't resolve environment variables")
		os.Exit(1)
	}

	ctrl.SetLogger(log.NewAdapter(logger))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             runtimescheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "8020e9ff.absa.oss",
	})
	if err != nil {
		logger.Err(err).Msg("unable to start manager")
		os.Exit(1)
	}

	logger.Info().Msg("registering components.")

	// Add external-dns DNSEndpoints resource
	// https://github.com/operator-framework/operator-sdk/blob/master/doc/user-guide.md#adding-3rd-party-resources-to-your-operator
	schemeBuilder := &scheme.Builder{GroupVersion: schema.GroupVersion{Group: "externaldns.k8s.io", Version: "v1alpha1"}}
	schemeBuilder.Register(&externaldns.DNSEndpoint{}, &externaldns.DNSEndpointList{})
	if err := schemeBuilder.AddToScheme(mgr.GetScheme()); err != nil {
		logger.Err(err).Msg("")
		os.Exit(1)
	}

	reconciler := &controllers.GslbReconciler{
		Config:      config,
		Client:      mgr.GetClient(),
		DepResolver: resolver,
		Scheme:      mgr.GetScheme(),
	}

	logger.Info().Msg("starting DNS provider")
	f, err = dns.NewDNSProviderFactory(reconciler.Client, *reconciler.Config)
	if err != nil {
		logger.Err(err).Msgf("unable to create factory (%s)", err)
		os.Exit(1)
	}
	reconciler.DNSProvider = f.Provider()
	logger.Info().Msgf("provider: %s", reconciler.DNSProvider)
	logger.Info().Msg("starting metrics")
	reconciler.Metrics = metrics.NewPrometheusMetrics(*reconciler.Config)
	err = reconciler.Metrics.Register()
	if err != nil {
		logger.Err(err).Msg("register metrics error")
		os.Exit(1)
	}
	if err = reconciler.SetupWithManager(mgr); err != nil {
		logger.Err(err).Msg("unable to create controller Gslb")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder
	logger.Info().Msg("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Err(err).Msg("problem running manager")
		os.Exit(1)
	}
	reconciler.Metrics.Unregister()
}
