package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/psvmcc/hub/pkg/misc"
	"github.com/psvmcc/hub/pkg/types"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func RubyGems(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "rubygems"

		requestedPath := strings.TrimPrefix(c.Param("*"), "/")
		upstreamPath := strings.TrimPrefix(path.Clean("/"+requestedPath), "/")
		cacheKey := upstreamPath
		if upstreamPath == "" || upstreamPath == "." {
			upstreamPath = ""
			cacheKey = "__root"
		}

		query := c.QueryString()
		cachePath := cacheKey
		if query != "" {
			sum := sha256.Sum256([]byte(query))
			cachePath = path.Join("_query", hex.EncodeToString(sum[:]), cacheKey)
		}

		upstreamBase := strings.TrimSuffix(cfg.Server.RUBYGEMS[key], "/")
		url := upstreamBase + "/"
		if upstreamPath != "" {
			url += upstreamPath
		}
		if query != "" {
			url = url + "?" + query
		}

		dest := fmt.Sprintf("%s/rubygems/%s/%s", cfg.Dir, key, cachePath)

		headers := types.RequestHeaders{
			"User-Agent": "rubygems",
		}

		cacheExists := true
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			cacheExists = false
		} else {
			isGemPath := strings.HasPrefix(upstreamPath, "gems/") && strings.HasSuffix(upstreamPath, ".gem")
			if isGemPath {
				c.Response().Header().Add("X-Cache-Status", "HIT")
				return c.File(dest)
			}

			equal, err := misc.FilesEqual(url, dest)
			if err != nil {
				logger.Named(loggerNS).Errorf("[FilesEqual]: %s", err)
			}

			if equal {
				c.Response().Header().Add("X-Cache-Status", "HIT")
				return c.File(dest)
			}
		}

		status, err := misc.DownloadFile(url, dest, headers)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Downloading] %s", err)
			if _, statErr := os.Stat(dest); errors.Is(statErr, os.ErrNotExist) {
				logger.Named(loggerNS).Errorf("[FS]: %s", statErr)
				c.Response().Header().Add("X-Cache-Status", "ERROR")
				return c.String(status, "Please check logs...")
			}
			c.Response().Header().Add("X-Cache-Status", "STALE")
			logger.Named(loggerNS).Debugf("Remote %s served from local file %s", url, dest)
			return c.File(dest)
		}

		if cacheExists {
			c.Response().Header().Add("X-Cache-Status", "EXPIRED")
		} else {
			c.Response().Header().Add("X-Cache-Status", "MISS")
		}
		logger.Named(loggerNS).Debugf("Remote %s saved as %s", url, dest)
		return c.File(dest)
	}
}
