package k8s

import (
	"log/slog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	k8sClient *kubernetes.Clientset
}

func NewClient() *Client {
	config, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("[k8s] error while setting up k8s client config!", slog.Any("error", err))
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		slog.Error("[k8s] error while setting up k8s clientset!", slog.Any("error", err))
	}
	return &Client{
		k8sClient: clientset,
	}
}
