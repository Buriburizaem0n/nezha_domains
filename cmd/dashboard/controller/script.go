package controller

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

//go:embed scripts/*
var scriptFS embed.FS

func serveScript(c *gin.Context) {
	name := c.Param("name")
	content, err := scriptFS.ReadFile(filepath.Join("scripts", name))

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Script not found"})
		return
	}

	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}

func serveAgentBinary(c *gin.Context) {
	osType := c.Param("os")
	arch := c.Param("arch")

	// Nezha agent releases are usually .zip
	// We will proxy the download from GitHub/Gitee and convert it to .tar.gz on the fly
	// This allows using 'tar' in the install script and avoids GitHub connectivity issues

	repo := "nezhahq/agent"
	// For simplicity, we use the latest release
	// In a real scenario, you might want to cache this or use a specific version
	zipUrl := fmt.Sprintf("https://github.com/%s/releases/latest/download/nezha-agent_%s_%s.zip", repo, osType, arch)

	resp, err := http.Get(zipUrl)
	if err != nil || resp.StatusCode != http.StatusOK {
		// Try Gitee if GitHub fails
		zipUrl = fmt.Sprintf("https://gitee.com/naibahq/agent/releases/latest/download/nezha-agent_%s_%s.zip", osType, arch)
		resp, err = http.Get(zipUrl)
		if err != nil || resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch agent binary from upstream"})
			return
		}
	}
	defer resp.Body.Close()

	// Create a temporary file to store the zip
	tmpZip, err := os.CreateTemp("", "nezha-agent-*.zip")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp file"})
		return
	}
	defer os.Remove(tmpZip.Name())
	defer tmpZip.Close()

	// Limit the download to 200MB to prevent DoS
	limitReader := io.LimitReader(resp.Body, 200*1024*1024)
	if _, err := io.Copy(tmpZip, limitReader); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save zip"})
		return
	}

	// Re-open for reading
	zipReader, err := zip.OpenReader(tmpZip.Name())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open zip"})
		return
	}
	defer zipReader.Close()

	// Set headers for .tar.gz
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=nezha-agent_%s_%s.tar.gz", osType, arch))

	gw := gzip.NewWriter(c.Writer)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Security check: Limit uncompressed size to 100MB per file
		if f.UncompressedSize64 > 100*1024*1024 {
			continue
		}

		header, err := tar.FileInfoHeader(f.FileInfo(), "")
		if err != nil {
			continue
		}
		header.Name = f.Name

		if err := tw.WriteHeader(header); err != nil {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		// Use LimitReader as a secondary defense for gosec (G110)
		if _, err := io.Copy(tw, io.LimitReader(rc, 100*1024*1024)); err != nil {
			rc.Close()
			continue
		}
		rc.Close()
	}
}
