package main

import (
	"flag"

	"os"

	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/util"
	"github.com/sky-uk/feed/util/cmd"
)

var (
	apiServer                  string
	caCertFile                 string
	tokenFile                  string
	clientCertFile             string
	clientKeyFile              string
	debug                      bool
	healthPort                 int
	elbLabelValue              string
	elbRegion                  string
	r53HostedZone              string
	pushgatewayURL             string
	pushgatewayIntervalSeconds int
)

var unhealthyCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: util.PrometheusNamespace,
	Subsystem: util.PrometheusDNSSubsystem,
	Name:      "unhealthy_time",
	Help:      "The number of seconds feed-dns has been unhealthy.",
})

func init() {
	const (
		defaultAPIServer                  = "https://kubernetes:443"
		defaultCaCertFile                 = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile                  = ""
		defaultClientCertFile             = ""
		defaultClientKeyFile              = ""
		defaultHealthPort                 = 12082
		defaultElbRegion                  = "eu-west-1"
		defaultElbLabelValue              = ""
		defaultHostedZone                 = ""
		defaultPushgatewayIntervalSeconds = 60
	)

	flag.StringVar(&apiServer, "apiserver", defaultAPIServer,
		"Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile,
		"File containing kubernetes ca certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile,
		"File containing kubernetes client authentication token.")
	flag.StringVar(&clientCertFile, "client-certfile", defaultClientCertFile,
		"File containing client certificate. Leave empty to not use a client certificate.")
	flag.StringVar(&clientKeyFile, "client-keyfile", defaultClientKeyFile,
		"File containing client key. Leave empty to not use a client certificate.")
	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller.")
	flag.StringVar(&elbRegion, "elb-region", defaultElbRegion,
		"AWS region for ELBs.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Alias to ELBs tagged with "+elb.ElbTag+"=value. Route53 entries will be created to these,"+
			"depending on the scheme.")
	flag.StringVar(&r53HostedZone, "r53-hosted-zone", defaultHostedZone,
		"Route53 hosted zone id to manage.")
	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")

	prometheus.MustRegister(unhealthyCounter)
}

func main() {
	flag.Parse()
	cmd.ConfigureLogging(debug)
	validateConfig()

	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiServer, clientCertFile, clientKeyFile)
	dnsUpdater := dns.New(r53HostedZone, elbRegion, elbLabelValue)

	controller := controller.New(controller.Config{
		KubernetesClient: client,
		Updaters:         []controller.Updater{dnsUpdater},
	})

	cmd.AddHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}

	cmd.AddUnhealthyLogger(controller, unhealthyCounter)
	cmd.AddMetricsPusher("feed-dns", pushgatewayURL, time.Second*time.Duration(pushgatewayIntervalSeconds))

	select {}
}

func validateConfig() {
	if r53HostedZone == "" {
		log.Error("Must supply r53-hosted-zone")
		os.Exit(-1)
	}
}
