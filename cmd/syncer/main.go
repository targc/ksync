package main

import (
	"context"
	"log"
	"os"
	"time"

	syncerpkg "github.com/targc/ksync/internal/syncer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
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

	k8s, err := buildK8sClient()
	if err != nil {
		log.Fatalf("failed to build k8s client: %v", err)
	}

	syncer := &syncerpkg.Syncer{
		APIURL:       apiURL,
		APIToken:     apiToken,
		IntervalSync: interval,
		K8s:          k8s,
	}

	if err := syncer.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func buildK8sClient() (dynamic.Interface, error) {
	var cfg *rest.Config
	var err error

	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, err
	}

	return dynamic.NewForConfig(cfg)
}
