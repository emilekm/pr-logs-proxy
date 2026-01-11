package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/Alliance-Community/pr-logs-proxy/internal/services"
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	adminLogFile      = "ra_adminlog.txt"
	joinLogFile       = "joinlog.log"
	playerProfileFile = "playerprofiles.log"
)

var port = flag.String("port", "50051", "port to listen on")
var insecure = flag.Bool("insecure", false, "use insecure connection (no TLS)")
var certPath = flag.String("cert", "", "path to TLS certificate")
var keyPath = flag.String("key", "", "path to TLS key")
var logsPath = flag.String("logs", "logs/", "path to logs directory")

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		panic(err)
	}
}

func run() error {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", *port))
	if err != nil {
		return err
	}

	opts := []grpc.ServerOption{}
	if !(*insecure) {
		if *certPath == "" || *keyPath == "" {
			return fmt.Errorf("cert and key paths must be provided in secure mode")
		}

		creds, err := credentials.NewServerTLSFromFile(*certPath, *keyPath)
		if err != nil {
			return err
		}
		opts = append(opts, grpc.Creds(creds))
	}

	gRPCserver := grpc.NewServer(opts...)

	adminLogService := services.NewAdminLogService(filepath.Join(*logsPath, adminLogFile))
	joinLogService := services.NewJoinLogService(filepath.Join(*logsPath, joinLogFile))
	playerProfilesService := services.NewPlayerProfilesService(filepath.Join(*logsPath, playerProfileFile))

	v1.RegisterAdminLogServiceServer(gRPCserver, adminLogService)
	v1.RegisterJoinLogServiceServer(gRPCserver, joinLogService)
	v1.RegisterPlayerProfileServiceServer(gRPCserver, playerProfilesService)

	slog.Info(fmt.Sprintf("Starting proxy on port %s", *port))
	return gRPCserver.Serve(lis)
}
