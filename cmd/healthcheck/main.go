// Package main 是容器健康检查入口。
//
// 运行镜像是 distroless，没有 shell/curl，因此用一个独立的小程序探测
// 本机 /healthz。它只读探活，不改变服务端行为。
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultURL = "http://127.0.0.1:8080/healthz"
	timeout    = 2 * time.Second
)

func main() {
	url := os.Getenv("KUBEDECK_HEALTHCHECK_URL")
	if url == "" {
		url = defaultURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := &http.Client{Timeout: timeout}
	if err := check(ctx, client, url); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

// check 对 url 发起 GET，只有 2xx 才算健康。
func check(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return nil
}
