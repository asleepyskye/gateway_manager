package api

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"pluralkit/manager/internal/core"
)

// TODO: document this function.
func (a *API) Ping(c *gin.Context) {
	c.String(http.StatusOK, "pong!")
}

// TODO: document this function.
func (a *API) GetStatus(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) ClusterRegister(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) ClusterDeregister(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) SetClusterStatus(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) GetConfig(c *gin.Context) {
	val, err := a.EtcdClient.Get(c, "gateway_config")
	if err != nil {
		c.String(http.StatusInternalServerError, "error while getting config from etcd")
		slog.Warn("[api] error while getting config", slog.Any("error", err))
		return
	}

	c.JSON(http.StatusOK, val)
}

// TODO: document this function.
func (a *API) SetConfig(c *gin.Context) {
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "error while getting body data")
		slog.Warn("[api] error while getting body data", slog.Any("error", err))
		return
	}

	err = a.Controller.SetConfig(data)
	if err != nil {
		c.String(http.StatusInternalServerError, "error while setting config")
		slog.Warn("[api] error while setting config", slog.Any("error", err))
		return
	}

	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) SetRollout(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) SetDeploy(c *gin.Context) {
	a.Controller.SendEvent(core.EventDeployCmd)
	c.String(http.StatusOK, "")
}
