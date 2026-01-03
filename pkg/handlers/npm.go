package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/psvmcc/hub/pkg/misc"
	"github.com/psvmcc/hub/pkg/types"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type npmCacheMeta struct {
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

const npmSearchTTL = 10 * time.Minute

func NpmProxy(key string) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := c.Get("cfg").(types.ConfigFile)
		logger := c.Get("logger").(*zap.SugaredLogger)
		loggerNS := "npm"

		rawPath := strings.TrimPrefix(c.Param("*"), "/")
		rawPath = strings.TrimSuffix(rawPath, "/")
		if rawPath == "" {
			return c.String(http.StatusNotFound, "")
		}

		cleaned := path.Clean("/" + rawPath)
		cleaned = strings.TrimPrefix(cleaned, "/")
		if cleaned == "" {
			return c.String(http.StatusNotFound, "")
		}

		if isNpmSearchPath(cleaned) {
			return handleNpmSearch(c, cfg, logger, loggerNS, key)
		}
		if isNpmTarballPath(cleaned) {
			return handleNpmTarball(c, cfg, logger, loggerNS, key, cleaned)
		}
		return handleNpmMetadata(c, cfg, logger, loggerNS, key, cleaned)
	}
}

func handleNpmMetadata(c echo.Context, cfg types.ConfigFile, logger *zap.SugaredLogger, loggerNS, key, rawPath string) error {
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		decodedPath = rawPath
	}
	decodedPath = path.Clean("/" + decodedPath)
	packageName := strings.TrimSuffix(strings.TrimPrefix(decodedPath, "/"), "/")
	if packageName == "" {
		return c.String(http.StatusNotFound, "")
	}

	acceptKey, upstreamAccept := npmAcceptHeader(c.Request().Header.Get("Accept"))
	query := c.QueryString()
	queryHash := ""
	if query != "" {
		sum := sha256.Sum256([]byte(query))
		queryHash = hex.EncodeToString(sum[:])
	}

	filenameBase := fmt.Sprintf("packument.%s", acceptKey)
	if queryHash != "" {
		filenameBase = fmt.Sprintf("%s.%s", filenameBase, queryHash)
	}
	packagePath := filepath.FromSlash(packageName)
	cacheDir := filepath.Join(cfg.Dir, "npm", key, "metadata", packagePath)
	dataFile := filepath.Join(cacheDir, filenameBase+".json")
	metaFile := filepath.Join(cacheDir, filenameBase+".meta.json")

	upstreamBase := strings.TrimSuffix(cfg.Server.NPM[key], "/")
	upstreamName := npmEncodePackageName(packageName)
	upstreamURL := fmt.Sprintf("%s/%s", upstreamBase, upstreamName)
	if query != "" {
		upstreamURL = upstreamURL + "?" + query
	}

	headers := types.RequestHeaders{
		"User-Agent": "npm",
		"Accept":     upstreamAccept,
	}

	cacheExists := fileExists(dataFile)
	meta := npmCacheMeta{}
	if cacheExists {
		meta, _ = readNpmCacheMeta(metaFile)
	}

	status, newETag, newLastModified, notModified, err := misc.DownloadFileConditional(upstreamURL, dataFile, headers, meta.ETag, meta.LastModified)
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
			if writeErr := writeNpmCacheMeta(metaFile, meta); writeErr != nil {
				logger.Named(loggerNS).Errorf("Cache meta write error: %s", writeErr)
			}
		}
	}

	payload, err := os.ReadFile(filepath.Clean(dataFile))
	if err != nil {
		logger.Named(loggerNS).Errorf("Cache read error: %s", err)
		return c.String(http.StatusBadRequest, "Metadata error")
	}

	var packument map[string]any
	if err := json.Unmarshal(payload, &packument); err != nil {
		logger.Named(loggerNS).Errorf("Metadata unmarshal error: %s", err)
		return c.String(http.StatusBadRequest, "Metadata error")
	}

	baseURL := fmt.Sprintf("%s://%s", c.Scheme(), c.Request().Host)
	rewrote := rewriteNpmTarballs(packument, baseURL, key)
	if !rewrote {
		logger.Named(loggerNS).Debugf("No tarball URLs rewritten for %s", packageName)
	}

	updated, err := json.Marshal(packument)
	if err != nil {
		logger.Named(loggerNS).Errorf("Metadata marshal error: %s", err)
		return c.String(http.StatusInternalServerError, "Metadata error")
	}

	return c.Blob(http.StatusOK, upstreamAccept, updated)
}

func handleNpmTarball(c echo.Context, cfg types.ConfigFile, logger *zap.SugaredLogger, loggerNS, key, rawPath string) error {
	upstreamBase := strings.TrimSuffix(cfg.Server.NPM[key], "/")
	upstreamURL := fmt.Sprintf("%s/%s", upstreamBase, rawPath)
	dest := filepath.Join(cfg.Dir, "npm", key, "tarballs", filepath.FromSlash(rawPath))

	headers := types.RequestHeaders{
		"User-Agent": "npm",
	}

	if _, err := os.Stat(dest); err == nil {
		c.Response().Header().Add("X-Cache-Status", "HIT")
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
		return c.File(dest)
	}

	c.Response().Header().Add("X-Cache-Status", "MISS")
	logger.Named(loggerNS).Debugf("Remote %s saved as %s", upstreamURL, dest)
	return c.File(dest)
}

func handleNpmSearch(c echo.Context, cfg types.ConfigFile, logger *zap.SugaredLogger, loggerNS, key string) error {
	query := c.QueryString()
	hash := "empty"
	if query != "" {
		sum := sha256.Sum256([]byte(query))
		hash = hex.EncodeToString(sum[:])
	}

	dest := filepath.Join(cfg.Dir, "npm", key, "search", hash+".json")
	info, err := os.Stat(dest)
	if err == nil && time.Since(info.ModTime()) < npmSearchTTL {
		c.Response().Header().Add("X-Cache-Status", "HIT")
		c.Response().Header().Set("Content-Type", "application/json")
		return c.File(dest)
	}

	upstreamBase := strings.TrimSuffix(cfg.Server.NPM[key], "/")
	upstreamURL := fmt.Sprintf("%s/-/v1/search", upstreamBase)
	if query != "" {
		upstreamURL = upstreamURL + "?" + query
	}

	headers := types.RequestHeaders{
		"User-Agent": "npm",
		"Accept":     "application/json",
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
		c.Response().Header().Set("Content-Type", "application/json")
		return c.File(dest)
	}

	if err := os.Chtimes(dest, time.Now(), time.Now()); err != nil {
		logger.Named(loggerNS).Errorf("Cache timestamp update error: %s", err)
	}
	c.Response().Header().Add("X-Cache-Status", "MISS")
	c.Response().Header().Set("Content-Type", "application/json")
	return c.File(dest)
}

func isNpmTarballPath(p string) bool {
	return strings.Contains(p, "/-/") && (strings.HasSuffix(p, ".tgz") || strings.HasSuffix(p, ".tar.gz"))
}

func isNpmSearchPath(p string) bool {
	return strings.TrimSuffix(p, "/") == "-/v1/search"
}

func npmAcceptHeader(accept string) (cacheKey, upstreamAccept string) {
	if strings.Contains(accept, "application/vnd.npm.install-v1+json") || accept == "" || strings.Contains(accept, "*/*") {
		return "corgi", "application/vnd.npm.install-v1+json"
	}
	if strings.Contains(accept, "application/json") {
		return "full", "application/json"
	}
	sum := sha256.Sum256([]byte(accept))
	return "accept-" + hex.EncodeToString(sum[:8]), accept
}

func npmEncodePackageName(name string) string {
	if strings.HasPrefix(name, "@") && strings.Contains(name, "/") {
		return strings.ReplaceAll(name, "/", "%2F")
	}
	return name
}

func rewriteNpmTarballs(packument map[string]any, baseURL, key string) bool {
	versionsRaw, ok := packument["versions"]
	if !ok {
		return false
	}
	versions, ok := versionsRaw.(map[string]any)
	if !ok {
		return false
	}

	updated := false
	for _, v := range versions {
		versionInfo, ok := v.(map[string]any)
		if !ok {
			continue
		}
		distRaw, ok := versionInfo["dist"]
		if !ok {
			continue
		}
		dist, ok := distRaw.(map[string]any)
		if !ok {
			continue
		}
		tarballRaw, ok := dist["tarball"]
		if !ok {
			continue
		}
		tarballURL, ok := tarballRaw.(string)
		if !ok || tarballURL == "" {
			continue
		}
		parsed, err := url.Parse(tarballURL)
		if err != nil || parsed.Path == "" {
			continue
		}
		rewritePath := parsed.Path
		if parsed.RawQuery != "" {
			rewritePath = rewritePath + "?" + parsed.RawQuery
		}
		dist["tarball"] = fmt.Sprintf("%s/npm/%s%s", baseURL, key, rewritePath)
		updated = true
	}

	return updated
}

func readNpmCacheMeta(metaPath string) (npmCacheMeta, error) {
	meta := npmCacheMeta{}
	data, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func writeNpmCacheMeta(metaPath string, meta npmCacheMeta) error {
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

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}
