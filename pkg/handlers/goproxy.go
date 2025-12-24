package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/psvmcc/hub/pkg/misc"
	"github.com/psvmcc/hub/pkg/types"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// downloadAndCacheFile downloads a file from upstream and caches it locally
func downloadAndCacheFile(c echo.Context, key, loggerNS, url, dest string) error {
	logger := c.Get("logger").(*zap.SugaredLogger)

	headers := types.RequestHeaders{
		"User-Agent": "go/goproxy",
	}

	status, err := misc.DownloadFile(url, dest, headers)
	if err != nil {
		logger.Named(loggerNS).Errorf("[Downloading] %s", err)
		if _, err = os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			logger.Named(loggerNS).Errorf("[FS]: %s", err)
			return c.String(status, "410 Gone\n")
		}
		c.Response().Header().Add("X-Cache-Status", "HIT")
		logger.Named(loggerNS).Debugf("Remote %s served from local file %s", url, dest)
	} else {
		c.Response().Header().Add("X-Cache-Status", "MISS")
		logger.Named(loggerNS).Debugf("Remote %s saved as %s", url, dest)
	}

	return nil
}

// GoProxyList handles GET /{module}/@v/list requests
func GoProxyList(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "goproxy_list"

		module := c.Param("*")
		module = strings.TrimSuffix(module, "/@v/list")

		url := fmt.Sprintf("%s/%s/@v/list", cfg.Server.GOPROXY[key], module)
		dest := fmt.Sprintf("%s/goproxy/%s/%s/@v/list", cfg.Dir, key, module)

		headers := types.RequestHeaders{
			"User-Agent": "go/goproxy",
		}

		status, err := misc.DownloadFile(url, dest, headers)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Downloading] %s", err)
			if _, err = os.Stat(dest); errors.Is(err, os.ErrNotExist) {
				logger.Named(loggerNS).Errorf("[FS]: %s", err)
				return c.String(status, "410 Gone\n")
			}
			c.Response().Header().Add("X-Cache-Status", "HIT")
			logger.Named(loggerNS).Debugf("Remote %s served from local file %s", url, dest)
		} else {
			c.Response().Header().Add("X-Cache-Status", "MISS")
			logger.Named(loggerNS).Debugf("Remote %s saved as %s", url, dest)
		}

		c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
		return c.File(dest)
	}
}

// GoProxyInfo handles GET /{module}/@v/{version}.info requests
func GoProxyInfo(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)

		module := c.Param("*")
		parts := strings.Split(module, "/@v/")
		if len(parts) != 2 {
			return c.String(http.StatusBadRequest, "invalid path")
		}
		modulePath := parts[0]
		version := strings.TrimSuffix(parts[1], ".info")

		url := fmt.Sprintf("%s/%s/@v/%s.info", cfg.Server.GOPROXY[key], modulePath, version)
		dest := fmt.Sprintf("%s/goproxy/%s/%s/@v/%s.info", cfg.Dir, key, modulePath, version)

		if err := downloadAndCacheFile(c, key, "goproxy_info", url, dest); err != nil {
			return err
		}

		c.Response().Header().Set("Content-Type", "application/json")
		return c.File(dest)
	}
}

// GoProxyMod handles GET /{module}/@v/{version}.mod requests
func GoProxyMod(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)

		module := c.Param("*")
		parts := strings.Split(module, "/@v/")
		if len(parts) != 2 {
			return c.String(http.StatusBadRequest, "invalid path")
		}
		modulePath := parts[0]
		version := strings.TrimSuffix(parts[1], ".mod")

		url := fmt.Sprintf("%s/%s/@v/%s.mod", cfg.Server.GOPROXY[key], modulePath, version)
		dest := fmt.Sprintf("%s/goproxy/%s/%s/@v/%s.mod", cfg.Dir, key, modulePath, version)

		if err := downloadAndCacheFile(c, key, "goproxy_mod", url, dest); err != nil {
			return err
		}

		c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
		return c.File(dest)
	}
}

// GoProxyZip handles GET /{module}/@v/{version}.zip requests
func GoProxyZip(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "goproxy_zip"

		module := c.Param("*")
		parts := strings.Split(module, "/@v/")
		if len(parts) != 2 {
			return c.String(http.StatusBadRequest, "invalid path")
		}
		modulePath := parts[0]
		version := strings.TrimSuffix(parts[1], ".zip")

		url := fmt.Sprintf("%s/%s/@v/%s.zip", cfg.Server.GOPROXY[key], modulePath, version)
		dest := fmt.Sprintf("%s/goproxy/%s/%s/@v/%s.zip", cfg.Dir, key, modulePath, version)

		headers := types.RequestHeaders{
			"User-Agent": "go/goproxy",
		}

		// Check if file exists and has valid content
		if _, err := os.Stat(dest); err == nil {
			c.Response().Header().Add("X-Cache-Status", "HIT")
			logger.Named(loggerNS).Debugf("Serving cached file %s", dest)
			c.Response().Header().Set("Content-Type", "application/zip")
			return c.File(dest)
		}

		status, err := misc.DownloadFile(url, dest, headers)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Downloading] %s", err)
			if _, err = os.Stat(dest); errors.Is(err, os.ErrNotExist) {
				logger.Named(loggerNS).Errorf("[FS]: %s", err)
				return c.String(status, "410 Gone\n")
			}
			logger.Named(loggerNS).Debugf("Remote %s served from local file %s", url, dest)
		} else {
			c.Response().Header().Add("X-Cache-Status", "MISS")
			logger.Named(loggerNS).Debugf("Remote %s saved as %s", url, dest)
		}

		c.Response().Header().Set("Content-Type", "application/zip")
		return c.File(dest)
	}
}

// GoProxyLatest handles GET /{module}/@latest requests
func GoProxyLatest(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "goproxy_latest"

		module := c.Param("*")
		module = strings.TrimSuffix(module, "/@latest")

		url := fmt.Sprintf("%s/%s/@latest", cfg.Server.GOPROXY[key], module)
		dest := fmt.Sprintf("%s/goproxy/%s/%s/@latest", cfg.Dir, key, module)

		headers := types.RequestHeaders{
			"User-Agent": "go/goproxy",
		}

		// @latest should be fetched more frequently, so check if file is older than 1 hour
		fileInfo, err := os.Stat(dest)
		cacheValid := false
		if err == nil {
			oneHourAgo := time.Now().Add(-1 * time.Hour)
			cacheValid = fileInfo.ModTime().After(oneHourAgo)
		}

		if !cacheValid {
			status, err := misc.DownloadFile(url, dest, headers)
			if err != nil {
				logger.Named(loggerNS).Errorf("[Downloading] %s", err)
				if _, err = os.Stat(dest); errors.Is(err, os.ErrNotExist) {
					logger.Named(loggerNS).Errorf("[FS]: %s", err)
					return c.String(status, "410 Gone\n")
				}
				c.Response().Header().Add("X-Cache-Status", "HIT")
				logger.Named(loggerNS).Debugf("Remote %s served from local file %s", url, dest)
			} else {
				c.Response().Header().Add("X-Cache-Status", "MISS")
				logger.Named(loggerNS).Debugf("Remote %s saved as %s", url, dest)
			}
		} else {
			c.Response().Header().Add("X-Cache-Status", "HIT")
			logger.Named(loggerNS).Debugf("Serving cached @latest for %s", module)
		}

		c.Response().Header().Set("Content-Type", "application/json")
		return c.File(dest)
	}
}
