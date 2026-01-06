package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/psvmcc/hub/pkg/misc"
	"github.com/psvmcc/hub/pkg/types"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type cargoCacheMeta struct {
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

type cargoIndexConfig struct {
	DL           string `json:"dl"`
	API          string `json:"api,omitempty"`
	AuthRequired bool   `json:"auth-required,omitempty"`
}

var cargoHopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func CargoIndex(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "cargo_index"

		source, ok := cfg.Server.Cargo[key]
		if !ok || source.Base == "" {
			return c.String(http.StatusNotFound, "")
		}
		endpoints, err := cargoEndpointsFromSource(source)
		if err != nil {
			logger.Named(loggerNS).Errorf("Config error: %s", err)
			return c.String(http.StatusInternalServerError, "")
		}

		rawPath := strings.TrimPrefix(c.Param("*"), "/")
		cleaned := path.Clean("/" + rawPath)
		cleaned = strings.TrimPrefix(cleaned, "/")
		if cleaned == "" {
			return c.String(http.StatusNotFound, "")
		}

		if cleaned == "config.json" {
			dest := filepath.Join(cfg.Dir, "cargo", key, "index", "config.json")
			if _, err = os.Stat(dest); err == nil {
				c.Response().Header().Add("X-Cache-Status", "HIT")
				c.Response().Header().Set("Content-Type", "application/json")
				return c.File(dest)
			}

			baseURL := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)
			payload := cargoIndexConfig{
				DL:  fmt.Sprintf("%s/cargo/%s/crates/{crate}/{version}/download", baseURL, key),
				API: fmt.Sprintf("%s/cargo/%s/api", baseURL, key),
			}
			data, errMarshal := json.Marshal(payload)
			if errMarshal != nil {
				logger.Named(loggerNS).Errorf("Config marshal error: %s", errMarshal)
				return c.String(http.StatusInternalServerError, "")
			}
			if err = os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
				logger.Named(loggerNS).Errorf("Config directory error: %s", err)
				return c.String(http.StatusInternalServerError, "")
			}
			if err = os.WriteFile(dest, data, 0o600); err != nil {
				logger.Named(loggerNS).Errorf("Config write error: %s", err)
				return c.String(http.StatusInternalServerError, "")
			}
			c.Response().Header().Add("X-Cache-Status", "MISS")
			c.Response().Header().Set("Content-Type", "application/json")
			return c.Blob(http.StatusOK, "application/json", data)
		}

		upstreamBase := strings.TrimSuffix(endpoints.Index, "/")
		upstreamURL := fmt.Sprintf("%s/%s", upstreamBase, cleaned)
		if query := c.QueryString(); query != "" {
			upstreamURL = upstreamURL + "?" + query
		}

		dest := filepath.Join(cfg.Dir, "cargo", key, "index", filepath.FromSlash(cleaned))
		metaFile := dest + ".meta.json"

		headers := types.RequestHeaders{
			"User-Agent": "cargo",
			"Accept":     "application/json",
		}

		cacheExists := cargoFileExists(dest)
		meta := cargoCacheMeta{}
		if cacheExists {
			meta, _ = readCargoCacheMeta(metaFile)
		}

		status, newETag, newLastModified, notModified, err := misc.DownloadFileConditional(upstreamURL, dest, headers, meta.ETag, meta.LastModified)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Downloading] %s", err)
			if !cacheExists {
				c.Response().Header().Add("X-Cache-Status", "ERROR")
				return c.String(status, "Please check logs...")
			}
			c.Response().Header().Add("X-Cache-Status", "STALE")
		} else {
			if notModified {
				c.Response().Header().Add("X-Cache-Status", "HIT")
			} else {
				c.Response().Header().Add("X-Cache-Status", "MISS")
			}
			if newETag != "" || newLastModified != "" {
				if newETag != "" {
					meta.ETag = newETag
				}
				if newLastModified != "" {
					meta.LastModified = newLastModified
				}
				if writeErr := writeCargoCacheMeta(metaFile, meta); writeErr != nil {
					logger.Named(loggerNS).Errorf("Cache meta write error: %s", writeErr)
				}
			}
		}

		c.Response().Header().Set("Content-Type", "application/json")
		return c.File(dest)
	}
}

func CargoCrateDownload(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "cargo_crates"

		source, ok := cfg.Server.Cargo[key]
		if !ok || source.Base == "" {
			return c.String(http.StatusNotFound, "")
		}
		endpoints, err := cargoEndpointsFromSource(source)
		if err != nil {
			logger.Named(loggerNS).Errorf("Config error: %s", err)
			return c.String(http.StatusInternalServerError, "")
		}

		crate := c.Param("crate")
		version := c.Param("version")
		if crate == "" || version == "" {
			return c.String(http.StatusNotFound, "")
		}

		upstreamBase := strings.TrimSuffix(endpoints.DL, "/")
		upstreamURL := fmt.Sprintf("%s/%s/%s/download", upstreamBase, crate, version)
		dest := filepath.Join(cfg.Dir, "cargo", key, "crates", crate, fmt.Sprintf("%s-%s.crate", crate, version))

		headers := types.RequestHeaders{
			"User-Agent": "cargo",
		}

		if _, err = os.Stat(dest); err == nil {
			c.Response().Header().Add("X-Cache-Status", "HIT")
			c.Response().Header().Set("Content-Type", "application/octet-stream")
			return c.File(dest)
		}

		status, err := misc.DownloadFile(upstreamURL, dest, headers)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Downloading] %s", err)
			if _, statErr := os.Stat(dest); errors.Is(statErr, os.ErrNotExist) {
				logger.Named(loggerNS).Errorf("[FS]: %s", statErr)
				c.Response().Header().Add("X-Cache-Status", "ERROR")
				return c.String(status, "Please check logs...")
			}
			c.Response().Header().Add("X-Cache-Status", "STALE")
			logger.Named(loggerNS).Debugf("Remote %s served from local file %s", upstreamURL, dest)
			c.Response().Header().Set("Content-Type", "application/octet-stream")
			return c.File(dest)
		}

		c.Response().Header().Add("X-Cache-Status", "MISS")
		logger.Named(loggerNS).Debugf("Remote %s saved as %s", upstreamURL, dest)
		c.Response().Header().Set("Content-Type", "application/octet-stream")
		return c.File(dest)
	}
}

func CargoAPIProxy(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "cargo_api"

		source, ok := cfg.Server.Cargo[key]
		if !ok || source.Base == "" {
			return c.String(http.StatusNotFound, "")
		}
		endpoints, err := cargoEndpointsFromSource(source)
		if err != nil {
			logger.Named(loggerNS).Errorf("Config error: %s", err)
			return c.String(http.StatusInternalServerError, "")
		}

		method := c.Request().Method
		if method != http.MethodGet && method != http.MethodHead {
			return c.NoContent(http.StatusMethodNotAllowed)
		}

		rawPath := strings.TrimPrefix(c.Param("*"), "/")
		cleaned := path.Clean("/" + rawPath)
		cleaned = strings.TrimPrefix(cleaned, "/")

		upstreamBase := strings.TrimSuffix(endpoints.API, "/")
		upstreamURL := upstreamBase
		if cleaned != "" {
			upstreamURL = fmt.Sprintf("%s/%s", upstreamBase, cleaned)
		}
		if query := c.QueryString(); query != "" {
			upstreamURL = upstreamURL + "?" + query
		}

		req, err := http.NewRequest(method, upstreamURL, http.NoBody)
		if err != nil {
			logger.Named(loggerNS).Errorf("Request build error: %s", err)
			return c.String(http.StatusBadRequest, "")
		}
		req.Header.Set("User-Agent", "cargo")
		if accept := c.Request().Header.Get("Accept"); accept != "" {
			req.Header.Set("Accept", accept)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			logger.Named(loggerNS).Errorf("[Proxy] %s", err)
			return c.String(http.StatusBadGateway, "")
		}
		defer resp.Body.Close()

		copyCargoHeaders(c.Response().Header(), resp.Header)
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}

		if method == http.MethodHead {
			return c.NoContent(resp.StatusCode)
		}
		return c.Stream(resp.StatusCode, contentType, resp.Body)
	}
}

func readCargoCacheMeta(metaPath string) (cargoCacheMeta, error) {
	meta := cargoCacheMeta{}
	data, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func writeCargoCacheMeta(metaPath string, meta cargoCacheMeta) error {
	file := filepath.Clean(metaPath)
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o600)
}

func cargoFileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func copyCargoHeaders(dst, src http.Header) {
	for key, values := range src {
		if _, skip := cargoHopByHopHeaders[http.CanonicalHeaderKey(key)]; skip {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

type cargoEndpoints struct {
	Index string
	DL    string
	API   string
}

func cargoEndpointsFromSource(source types.CargoSource) (cargoEndpoints, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(source.Base), "/")
	if trimmed == "" {
		return cargoEndpoints{}, errors.New("empty cargo base URL")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return cargoEndpoints{}, fmt.Errorf("invalid cargo base URL: %q", source.Base)
	}

	api := trimmed + "/api"
	dl := trimmed + "/api/v1/crates"
	index := trimmed + "/index"

	if source.API != "" {
		api = source.API
	}
	if source.DL != "" {
		dl = source.DL
	}
	if source.Index != "" {
		index = source.Index
	}

	return cargoEndpoints{
		Index: index,
		DL:    dl,
		API:   api,
	}, nil
}
