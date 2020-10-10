package main

import (
	"context"
	"github.com/deinstapel/cf-dns-bot/domainmanager"
	"github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const ManagedLabel = "domainmanager.deinstapel.de"


func main() {
	logrus.SetFormatter(&logrus.TextFormatter{})
	logger := logrus.WithField("app", "dns-bot")

	var config *rest.Config
	var err error
	if kubeconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize kubeconfig")
		os.Exit(1)
	}
	clientSet := kubernetes.NewForConfigOrDie(config)

	// Initializing DomainHandler
	domainHandlerList := make([]*domainmanager.DomainManager, 0)

	cfApiKey, cfKeyOk := os.LookupEnv("CF_API_KEY")
	cfApiMail, cfMailOk := os.LookupEnv("CF_API_EMAIL")
	awsAccessKeyId, awsAccessKeyOk := os.LookupEnv("AWS_ACCESS_KEY_ID")
	awsSecretAccessKey, awsSecretKeyOk := os.LookupEnv("AWS_SECRET_ACCESS_KEY")
	if cfKeyOk && cfMailOk {
		cloudflareDomainHandler, err := domainmanager.CreateCloudflareDomainHandler(cfApiMail, cfApiKey)
		if err != nil {
			logger.WithError(err).Fatal("Could not initialize CloudFlare domain handler")
			os.Exit(1)
		}


		domainHandlerList = append(
			domainHandlerList,
			domainmanager.CreateDomainMananger(cloudflareDomainHandler, logger),
		)
	}
	if awsAccessKeyOk && awsSecretKeyOk {
		awsDomainHandler := domainmanager.CreateRoute53RouteHandler(awsAccessKeyId, awsSecretAccessKey)

		domainHandlerList = append(
			domainHandlerList,
			domainmanager.CreateDomainMananger(awsDomainHandler, logger),
		)
	}

	if (!cfKeyOk || !cfMailOk) && (!awsAccessKeyOk || !awsSecretKeyOk) {
		dummyDomainHandler := domainmanager.CreateDummyHandler()
		domainHandlerList = append(
			domainHandlerList,
			domainmanager.CreateDomainMananger(dummyDomainHandler, logger),
		)
	}

	signalChan := make(chan os.Signal)
	stopper, cancel := context.WithCancel(context.Background())
	go func() {
		<-signalChan
		cancel()
	}()
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	labelSelector := labels.Set(map[string]string{ManagedLabel: "yes"}).AsSelector()
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector.String()
			return clientSet.CoreV1().Nodes().List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector.String()
			return clientSet.CoreV1().Nodes().Watch(context.Background(),options)
		},
	}, &corev1.Node{}, 0, cache.Indexers{})

	nodeHandler := domainmanager.CreateNodeHandler(domainHandlerList, logger)
	informer.AddEventHandler(nodeHandler)
	informer.Run(stopper.Done())
}
