package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"pluralkit/manager/internal/core"
)

// TODO: document this function.
func (a *API) Ping(c *gin.Context) {
	c.String(http.StatusOK, "pong!")
}

// TODO: document this function.
func (a *API) GetStatus(c *gin.Context) {
	shards := a.Controller.GetShardStatus()
	val, err := json.Marshal(shards)
	if err != nil {
		c.String(http.StatusInternalServerError, "error while marshalling shard status")
		slog.Warn("[api] error while marshalling status", slog.Any("error", err))
		return
	}
	c.Data(http.StatusOK, "application/json", val)
}

// TODO: document this function.
func (a *API) SetShardStatus(c *gin.Context) {
	var state core.ShardState
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "error while reading request body")
		slog.Warn("[api] error while reading request body in SetClusterStatus", slog.Any("error", err))
		return
	}
	err = json.Unmarshal(data, &state)
	if err != nil {
		c.String(http.StatusBadRequest, "error while parsing status")
		slog.Warn("[api] error while parsing cluster status", slog.Any("error", err))
		return
	}
	a.Controller.UpdateShardStatus(state)
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

// TODO: document this function.
func (a *API) GetCache(c *gin.Context) {
	guildID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.String(http.StatusInternalServerError, "")
		return
	}
	path := "/guilds/" + strconv.Itoa(guildID) + strings.TrimRight(c.Param("path"), "/")

	shardID := (guildID >> 22) % *a.NumShards
	clusterID := shardID / a.Config.MaxConcurrency

	target := (*a.CacheEndpoints)[clusterID] + path

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
