package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gin-gonic/gin"

	"pluralkit/manager/internal/api"
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"
)

func main() {
	//setup our signal handler
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	var wg sync.WaitGroup

	//etcd client
	slog.Info("setting up etcd client")
	etcdCli := etcd.NewClient("127.0.0.1:2379") //TODO: don't hardcode this
	if etcdCli == nil {
		os.Exit(1) //we print the error in the client, so just exit here
	}
	defer etcdCli.Close()

	//http api
	//this could be replaced with the default go http handler since it's not overly complex
	//just wanted to quickly throw together the api since it isn't the primary focus here
	slog.Info("setting up http api")
	gin.SetMode(gin.ReleaseMode) //just gonna set this here for now
	addr := ":5005"
	router := gin.Default()
	api.SetupRoutes(router)

	slog.Info("starting http api on", slog.String("address", addr))
	go func() {
		router.Run(addr)
	}()

	//state machine
	slog.Info("setting up control loop")
	machine := core.NewMachine(etcdCli)

	slog.Info("starting control loop")
	wg.Add(1)
	go machine.Run(&wg)

	//wait until sigint/sigterm and safely shutdown
	sig := <-quit
	slog.Info("shutting down, recieved", slog.String("signal", sig.String()))
	wg.Wait()
}
