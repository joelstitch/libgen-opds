package server

import (
	"context"
	"net/http"
	"time"

	"os"
	"reichard.io/libgen-opds/api"
	"reichard.io/libgen-opds/mirrors"
)

type Server struct {
	API        *api.API
	httpServer *http.Server
}

func NewServer() *Server {
	resolver := mirrors.New()
	api := api.NewApi(resolver)

	return &Server{
		API: api,
	}
}

func (s *Server) StartServer() {
	iface := os.Getenv("API_INTERFACE")
	if iface == "" {
		iface = "127.0.0.1"
	}
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "5144"
	}

	listenAddr := (iface + ":" + port)

	s.httpServer = &http.Server{
		Handler:           s.API,
		Addr:              listenAddr,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go s.httpServer.ListenAndServe()
}

func (s *Server) StopServer() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.httpServer.Shutdown(ctx)
}
