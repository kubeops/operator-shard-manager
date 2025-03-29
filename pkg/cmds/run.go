/*
Copyright AppsCode Inc. and Contributors.

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

package cmds

import (
	"crypto/tls"
	"os"
	"path/filepath"

	shardapi "kubeops.dev/operator-shard-manager/api/v1alpha1"
	"kubeops.dev/operator-shard-manager/pkg/controller"

	"github.com/spf13/cobra"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(shardapi.AddToScheme(scheme))
	utilruntime.Must(apiregistrationv1.AddToScheme(scheme))
}

func NewCmdRun() *cobra.Command {
	var metricsAddr string
	var certDir string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	cmd := &cobra.Command{
		Use:               "run",
		Short:             "Launch operator-shard-manager",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			ctrl.SetLogger(klog.NewKlogr())

			// if the enable-http2 flag is false (the default), http/2 should be disabled
			// due to its vulnerabilities. More specifically, disabling http/2 will
			// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
			// Rapid Reset CVEs. For more information see:
			// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
			// - https://github.com/advisories/GHSA-4374-p667-p6c8
			disableHTTP2 := func(c *tls.Config) {
				setupLog.Info("disabling http/2")
				c.NextProtos = []string{"http/1.1"}
			}

			if !enableHTTP2 {
				tlsOpts = append(tlsOpts, disableHTTP2)
			}

			// Create watchers for certificates
			var certWatcher *certwatcher.CertWatcher

			// Initial TLS options
			webhookTLSOpts := tlsOpts
			metricsTLSOpts := tlsOpts

			if len(certDir) > 0 {
				setupLog.Info("Initializing certificate watcher using provided certificates",
					"cert-dir", certDir, "cert-name", core.TLSCertKey, "cert-key", core.TLSPrivateKeyKey)

				var err error
				certWatcher, err = certwatcher.New(
					filepath.Join(certDir, core.TLSCertKey),
					filepath.Join(certDir, core.TLSPrivateKeyKey),
				)
				if err != nil {
					setupLog.Error(err, "Failed to initialize webhook certificate watcher")
					os.Exit(1)
				}

				webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
					config.GetCertificate = certWatcher.GetCertificate
				})

				metricsTLSOpts = append(metricsTLSOpts, func(config *tls.Config) {
					config.GetCertificate = certWatcher.GetCertificate
				})
			}

			webhookServer := webhook.NewServer(webhook.Options{
				TLSOpts: webhookTLSOpts,
			})

			// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
			// More info:
			// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/metrics/server
			// - https://book.kubebuilder.io/reference/metrics.html
			metricsServerOptions := metricsserver.Options{
				BindAddress:   metricsAddr,
				SecureServing: secureMetrics,
				TLSOpts:       metricsTLSOpts,
			}

			if secureMetrics {
				// FilterProvider is used to protect the metrics endpoint with authn/authz.
				// These configurations ensure that only authorized users and service accounts
				// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
				// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/metrics/filters#WithAuthenticationAndAuthorization
				metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
			}

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				Metrics:                metricsServerOptions,
				WebhookServer:          webhookServer,
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         enableLeaderElection,
				LeaderElectionID:       "8bd6a167.k8s.appscode.com",
				// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
				// when the Manager ends. This requires the binary to immediately end when the
				// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
				// speeds up voluntary leader transitions as the new leader don't have to wait
				// LeaseDuration time first.
				//
				// In the default scaffold provided, the program ends immediately after
				// the manager stops, so would be fine to enable this option. However,
				// if you are doing or is intended to do any operation such as perform cleanups
				// after the manager stops then its usage might be unsafe.
				// LeaderElectionReleaseOnCancel: true,
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}

			discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
			if err != nil {
				setupLog.Error(err, "unable to create discovery client")
				os.Exit(1)
			}
			mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

			if err = (&controller.RestMapperRefresher{Mapper: mapper}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "APIService")
				os.Exit(1)
			}

			if err = controller.NewShardConfigurationReconciler(mgr, discoveryClient, mapper).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ShardConfiguration")
				os.Exit(1)
			}

			if certWatcher != nil {
				setupLog.Info("Adding certificate watcher to manager")
				if err := mgr.Add(certWatcher); err != nil {
					setupLog.Error(err, "unable to add certificate watcher to manager")
					os.Exit(1)
				}
			}

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			setupLog.Info("starting manager")
			if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
				setupLog.Error(err, "problem running manager")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().StringVar(&certDir, "cert-dir", "", "The directory that contains the webhook and metrics server certificate.")
	cmd.Flags().BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	return cmd
}
