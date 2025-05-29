package main

import (
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	slogmulti "github.com/samber/slog-multi"

	"pluralkit/manager/internal/api"
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"
	"pluralkit/manager/internal/k8s"
)

func main() {
	//load config from envs
	var cfg core.ManagerConfig
	err := env.Parse(&cfg)
	if err != nil {
		slog.Error("error while loading envs!", slog.Any("error", err))
		os.Exit(1)
	}

	//setup logger
	var logger *slog.Logger
	if len(cfg.SentryURL) > 0 {
		err = sentry.Init(sentry.ClientOptions{
			Dsn: cfg.SentryURL,
		})
		if err != nil {
			log.Fatal(err)
		}
		defer sentry.Flush(2 * time.Second)

		logger = slog.New(slogmulti.Fanout(
			sentryslog.Option{Level: slog.Level(cfg.SentryLogLevel)}.NewSentryHandler(),
			slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.Level(cfg.LogLevel),
			}),
		))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.Level(cfg.LogLevel),
		}))
	}

	//seed rand
	rand.New(rand.NewSource(time.Now().UnixNano()))

	//setup our signal handler
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	var wg sync.WaitGroup

	//setup our event channel
	eventChannel := make(chan core.Event)

	//etcd client
	logger.Info("setting up etcd client")
	etcdCli, err := etcd.NewClient(cfg.EtcdAddr)
	if err != nil {
		logger.Error("error while setting up etcd client!", slog.Any("error", err))
		os.Exit(1)
	}
	defer etcdCli.Close()

	//k8s client
	logger.Info("setting up k8s client")
	k8sCli, err := k8s.NewClient(cfg.ManagerNamespace, "pluralkit-gateway_manager")
	if err != nil {
		logger.Error("error while setting up k8s client!", slog.Any("error", err))
		os.Exit(1)
	}

	//state machine
	logger.Info("setting up control FSM")
	controller := core.NewController(etcdCli, k8sCli, eventChannel, cfg, logger)

	logger.Info("starting control FSM")
	wg.Add(1)
	go controller.Run(&wg)

	//http api
	logger.Info("setting up http api")
	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(render.SetContentType(render.ContentTypeJSON))

	apiInstance := api.NewAPI(etcdCli, controller, cfg, logger)
	apiInstance.SetupRoutes(router)

	logger.Info("starting http api on", slog.String("address", cfg.BindAddr))
	go func() {
		err := http.ListenAndServe(cfg.BindAddr, router)
		if err != nil {
			logger.Error("error while running http router!", slog.Any("error", err))
		}
	}()

	//wait until sigint/sigterm and safely shutdown
	sig := <-quit
	logger.Info("shutting down, recieved", slog.String("signal", sig.String()))
	wg.Wait()
}
