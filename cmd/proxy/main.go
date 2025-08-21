package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	slogmulti "github.com/samber/slog-multi"

	"pluralkit/manager/internal/core"
	Proxy "pluralkit/manager/internal/proxy"
)

func main() {
	//load config from envs
	var cfg core.ProxyConfig
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

	//http proxy
	logger.Info("setting up http proxy")
	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(render.SetContentType(render.ContentTypeJSON))

	proxyInstance := Proxy.NewProxy(cfg, logger)
	proxyInstance.SetupRoutes(router)

	logger.Info("starting http proxy on", slog.String("address", cfg.BindAddr))
	err = http.ListenAndServe(cfg.BindAddr, router)
	if err != nil {
		logger.Error("error while running http router!", slog.Any("error", err))
	}
}
