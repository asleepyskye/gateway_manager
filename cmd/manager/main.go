package main

import (
    "log/slog"
    "time"

    "github.com/gin-gonic/gin"
    etcdClient "go.etcd.io/etcd/client/v3"
    
    "pluralkit/manager/internal/api"
    "pluralkit/manager/internal/core"
)

func main() {
    //etcd client
    slog.Info("setting up etcd client")
    cli, err := etcdClient.New(etcdClient.Config{
        Endpoints: []string{"localhost:2379"},
        DialTimeout: 5 * time.Second,
    })
    if err != nil {
        slog.Error("error while setting up etcd!", err) //TODO: properly handle etcd errors
    }
    _ = cli

    //http api
    slog.Info("setting up http api")
    addr := ":5005"
    router := gin.Default()
    api.SetupRoutes(router)

    slog.Info("starting http api on", slog.String("address", addr))
    go func() {
        router.Run(addr)
    }()

    //state machine
    slog.Info("setting up control loop")
    machine := core.NewMachine();

    slog.Info("starting control loop")
    go func() {
        machine.Run()
    }()

    select {}
}