package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/gin-gonic/gin"

	"pluralkit/manager/internal/api"
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"
	"pluralkit/manager/internal/k8s"
)

type config struct {
	EtcdAddr string `env:"pluralkit__manager__etcd_addr"`
	BindAddr string `env:"pluralkit__manager__addr"`
}

func main() {
	//load config from envs
	var cfg config
	err := env.Parse(&cfg)
	if err != nil {
		slog.Error("error while loading envs!", slog.Any("error", err))
	}

	//setup our signal handler
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	var wg sync.WaitGroup

	//setup our event channel
	eventChannel := make(chan core.Event)

	//etcd client
	slog.Info("setting up etcd client")
	etcdCli := etcd.NewClient(cfg.EtcdAddr)
	if etcdCli == nil {
		os.Exit(1) //we print the error in the client, so just exit here
	}
	defer etcdCli.Close()

	//k8s client
	slog.Info("setting up k8s client")
	k8sCli := k8s.NewClient("pluralkit-gateway", "created-by=pluralkit-gateway_manager") //prob add an env for namespace and label selectors?
	if k8sCli == nil {
		os.Exit(1)
	}

	//state machine
	slog.Info("setting up control FSM")
	controller := core.NewController(etcdCli, k8sCli, eventChannel)

	slog.Info("starting control FSM")
	wg.Add(1)
	go controller.Run(&wg)

	//http api
	//this could be replaced with the default go http handler since it's not overly complex
	//just wanted to quickly throw together the api since it isn't the primary focus here
	slog.Info("setting up http api")
	gin.SetMode(gin.ReleaseMode) //just gonna set this here for now
	router := gin.Default()
	apiInstance := api.NewAPI(etcdCli, controller)
	apiInstance.SetupRoutes(router)

	slog.Info("starting http api on", slog.String("address", cfg.BindAddr))
	go func() {
		err := router.Run(cfg.BindAddr)
		if err != nil {
			slog.Error("error while running http router!", slog.Any("error", err))
		}
	}()

	//wait until sigint/sigterm and safely shutdown
	sig := <-quit
	slog.Info("shutting down, recieved", slog.String("signal", sig.String()))
	wg.Wait()
}
