package kube

import (
	"context"
	"io"
	"strconv"
)

// Logs 读取或跟随 Pod 容器日志。使用独立的无总超时客户端，由 ctx 控制生命周期。
func (t *DirectTransport) Logs(ctx context.Context, namespace, name string, opts LogOptions) (io.ReadCloser, error) {
	req := t.logRest.Get().AbsPath("api", "v1", "namespaces", namespace, "pods", name, "log").
		Param("container", opts.Container)
	if opts.TailLines != nil {
		req = req.Param("tailLines", strconv.FormatInt(*opts.TailLines, 10))
	}
	if opts.SinceSeconds != nil {
		req = req.Param("sinceSeconds", strconv.FormatInt(*opts.SinceSeconds, 10))
	}
	if opts.SinceTime != "" {
		req = req.Param("sinceTime", opts.SinceTime)
	}
	if opts.Previous {
		req = req.Param("previous", "true")
	}
	if opts.Follow {
		req = req.Param("follow", "true")
	}
	if opts.Timestamps {
		req = req.Param("timestamps", "true")
	}
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}
