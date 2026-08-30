package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kacperkwapisz/fob/internal/app"
	"github.com/kacperkwapisz/fob/internal/httpx"
)

var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "exit 0 if GET /health succeeds")
	flag.Parse()
	if *healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8317"
		}
		res, err := http.Get("http://127.0.0.1:" + port + "/health")
		if err != nil || res.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	booted, err := app.Create(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	seed, _, err := booted.Panel.EnsureSeed()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if seed != "" {
		fmt.Printf("panel seed password: %s — change it on first login\n", seed)
	}
	go func() {
		_ = booted.Fob.Prices.Refresh(false)
		t := time.NewTicker(24 * time.Hour)
		for range t.C {
			_ = booted.Fob.Prices.Refresh(false)
		}
	}()
	go func() {
		t := time.NewTicker(6 * time.Hour)
		for range t.C {
			_, _ = booted.Fob.Usage.Purge()
		}
	}()
	addr := net.JoinHostPort(booted.Env.Host, strconv.Itoa(booted.Env.Port))
	srv := httpx.Server(addr, booted.Handler)
	fmt.Printf("fob %s listening on http://%s\n", version, addr)
	log.Fatal(srv.ListenAndServe())
}
