package misc

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/psvmcc/hub/pkg/types"
)

func DownloadFileConditional(url, destination string, headers types.RequestHeaders, etag, lastModified string) (code int, newETag, newLastModified string, notModified bool, err error) {
	client := &http.Client{}

	var req *http.Request
	var response *http.Response

	req, err = http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		code = http.StatusBadRequest
		return code, "", "", false, err
	}
	req.Header.Set("User-Agent", "hub")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	response, err = client.Do(req)
	if err != nil {
		code = http.StatusBadGateway
		return code, "", "", false, err
	}
	defer response.Body.Close()

	newETag = response.Header.Get("ETag")
	newLastModified = response.Header.Get("Last-Modified")

	if response.StatusCode == http.StatusNotModified {
		code = response.StatusCode
		return code, newETag, newLastModified, true, nil
	}

	if response.StatusCode != http.StatusOK {
		err = fmt.Errorf("upstream returned %s", response.Status)
		code = response.StatusCode
		return code, newETag, newLastModified, false, err
	}

	if err = os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		err = fmt.Errorf("failed to create destination directory: %v", err)
		code = http.StatusConflict
		return code, newETag, newLastModified, false, err
	}

	n, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		code = http.StatusInternalServerError
		return code, newETag, newLastModified, false, err
	}

	tempFileName := fmt.Sprintf(".tmp.%s.%d.%d", filepath.Base(filepath.Clean(destination)), time.Now().UnixNano(), n.Int64())
	tempFilePath := filepath.Join(filepath.Dir(filepath.Clean(destination)), tempFileName)
	tempFile, err := os.Create(filepath.Clean(tempFilePath))
	if err != nil {
		err = fmt.Errorf("failed to create temporary file: %v", err)
		code = http.StatusInternalServerError
		return code, newETag, newLastModified, false, err
	}
	defer os.Remove(tempFile.Name())

	_, err = io.Copy(tempFile, response.Body)
	if err != nil {
		err = fmt.Errorf("failed to copy response body to file: %v", err)
		code = http.StatusBadRequest
		return code, newETag, newLastModified, false, err
	}

	if newLastModified != "" {
		var lastModifiedTime time.Time
		if lastModifiedTime, err = time.Parse(http.TimeFormat, newLastModified); err == nil {
			var fileInfo os.FileInfo
			fileInfo, err = tempFile.Stat()
			if err != nil {
				err = fmt.Errorf("failed to get file info: %v", err)
				code = http.StatusInternalServerError
				return code, newETag, newLastModified, false, err
			}
			if err = os.Chtimes(tempFilePath, fileInfo.ModTime(), lastModifiedTime); err != nil {
				err = fmt.Errorf("failed to set last-modified time: %v", err)
				code = http.StatusInternalServerError
				return code, newETag, newLastModified, false, err
			}
		}
	}
	err = tempFile.Close()
	if err != nil {
		err = fmt.Errorf("tempFile close error: %v", err)
		code = http.StatusInternalServerError
		return code, newETag, newLastModified, false, err
	}

	if err := os.Rename(tempFile.Name(), destination); err != nil {
		err = fmt.Errorf("failed to rename temporary file to destination: %v", err)
		code = http.StatusInternalServerError
		return code, newETag, newLastModified, false, err
	}

	code = http.StatusOK
	return code, newETag, newLastModified, false, nil
}
