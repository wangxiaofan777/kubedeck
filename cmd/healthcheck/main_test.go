package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := check(context.Background(), srv.Client(), srv.URL); err != nil {
		t.Fatalf("check healthy endpoint: %v", err)
	}
}

func TestCheckUnhealthyStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := check(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("check must fail on 503")
	}
}

func TestCheckTimeout(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(done)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 20 * time.Millisecond}
	if err := check(context.Background(), client, srv.URL); err == nil {
		t.Fatal("check must fail when the endpoint times out")
	}
	<-done
}
