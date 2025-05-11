package api

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"

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
func (a *API) SetClusterStatus(c *gin.Context) {
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) GetConfig(c *gin.Context) {
	val, err := a.Controller.GetConfig()
	if err != nil {
		c.String(http.StatusInternalServerError, "error while getting config from control")
		slog.Warn("[api] error while getting config", slog.Any("error", err))
		return
	}

	c.Data(http.StatusOK, "application/json", val)
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
	a.Controller.SendEvent(core.EventRolloutCmd)
	c.String(http.StatusOK, "")
}

// TODO: document this function.
func (a *API) SetDeploy(c *gin.Context) {
	a.Controller.SendEvent(core.EventDeployCmd)
	c.String(http.StatusOK, "")
}

func (a *API) GetCache(c *gin.Context) {
	clusterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.String(http.StatusInternalServerError, "")
		return
	}
	path := c.Param("path")

	target := (*a.CacheEndpoints)[clusterID] + path

	//TODO: make this an actual proxy, this is just for testing purposes for now!!
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "")
		return
	}
	for header, values := range c.Request.Header {
		for _, value := range values {
			req.Header.Add(header, value)
		}
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		c.String(http.StatusInternalServerError, "")
		return
	}
	defer resp.Body.Close()
	c.Status(resp.StatusCode)
	for header, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(header, value)
		}
	}

	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "")
		return
	}
}
