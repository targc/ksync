package main

import (
	"context"
	"log"
	"os"
	"time"

	syncerpkg "github.com/targc/ksync/internal/syncer"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		log.Fatal("API_URL is required")
	}

	apiToken := os.Getenv("API_TOKEN")
	if apiToken == "" {
		log.Fatal("API_TOKEN is required")
	}

	interval := 5 * time.Second
	if s := os.Getenv("INTERVAL_SYNC"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			log.Fatalf("invalid INTERVAL_SYNC: %v", err)
		}
		interval = d
	}

	k8sCfg, k8s, mapper, err := buildK8sClient()
	if err != nil {
		log.Fatalf("failed to build k8s client: %v", err)
	}

	syncer := &syncerpkg.Syncer{
		APIURL:       apiURL,
		APIToken:     apiToken,
		IntervalSync: interval,
		K8s:          k8s,
		Mapper:       mapper,
		K8sConfig:    k8sCfg,
	}

	if err := syncer.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func buildK8sClient() (*rest.Config, dynamic.Interface, apimeta.RESTMapper, error) {
	var cfg *rest.Config
	var err error

	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, nil, nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return cfg, dynamicClient, mapper, nil
}
