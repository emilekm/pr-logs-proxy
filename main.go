package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"

	"github.com/Alliance-Community/pr-logs-proxy/internal/services/logs"
	"github.com/Alliance-Community/pr-logs-proxy/internal/services/players"
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

const (
	cpuProfileFile = "cpu.prof"
)

func main() {
	f, err := os.Create(cpuProfileFile)
	if err != nil {
		log.Fatal(err)
	}
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()
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

	// Create log services (loads all entries into memory)
	adminLogService, err := logs.NewAdminLogService(filepath.Join(*logsPath, adminLogFile))
	if err != nil {
		return fmt.Errorf("failed to create admin log service: %w", err)
	}
	slog.Info(fmt.Sprintf("Admin log service loaded with %d entries", adminLogService.GetEntryCount()))

	joinLogService, err := logs.NewJoinLogService(filepath.Join(*logsPath, joinLogFile))
	if err != nil {
		return fmt.Errorf("failed to create join log service: %w", err)
	}
	slog.Info(fmt.Sprintf("Join log service loaded with %d entries", joinLogService.GetEntryCount()))

	playerProfilesService, err := logs.NewPlayerProfilesService(filepath.Join(*logsPath, playerProfileFile))
	if err != nil {
		return fmt.Errorf("failed to create player profiles service: %w", err)
	}
	slog.Info(fmt.Sprintf("Player profiles service loaded with %d entries", playerProfilesService.GetEntryCount()))

	v1.RegisterAdminLogServiceServer(gRPCserver, adminLogService)
	v1.RegisterJoinLogServiceServer(gRPCserver, joinLogService)
	v1.RegisterPlayerProfilesServiceServer(gRPCserver, playerProfilesService)

	// Create and register PlayerQueryService (uses service interfaces directly)
	playerQueryService := players.NewPlayerQueryService(
		adminLogService,
		joinLogService,
		playerProfilesService,
	)
	v1.RegisterPlayerQueryServiceServer(gRPCserver, playerQueryService)
	if err := playerQueryService.Start(); err != nil {
		slog.Error("PlayerQueryService failed", slog.Any("error", err))
	}

	slog.Info(fmt.Sprintf("Starting proxy on port %s", *port))
	go func() {
		if err := gRPCserver.Serve(lis); err != nil {
			slog.Error("gRPC server failed", slog.Any("error", err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	slog.Info("Shutting down")
	gRPCserver.Stop()
	return nil
}
