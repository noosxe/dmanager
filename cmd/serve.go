package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	connect "connectrpc.com/connect"
	"github.com/spf13/cobra"

	"dmanager/internal/auth"
	"dmanager/internal/config"
	"dmanager/internal/container"
	"dmanager/internal/db"
	"dmanager/internal/docker"
	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
	"dmanager/internal/logging"
)

var (
	port       string
	dbPath     string
	configPath string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the dmanager backend daemon",
	Long:  "Starts the ConnectRPC server hosting the dmanager services.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize structured logging
		var logHandler slog.Handler
		if os.Getenv("APP_ENV") == "production" || os.Getenv("DMANAGER_ENV") == "production" {
			logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
		} else {
			logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
		}
		logger := slog.New(logHandler)
		slog.SetDefault(logger)

		cmdLogger := logger.With("module", "cmd")

		// Load configuration
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Apply CLI overrides if explicitly changed
		dbFile := cfg.Server.DBPath
		if cmd.Flags().Changed("db") {
			dbFile = dbPath
		}

		listenPort := cfg.Server.Port
		if cmd.Flags().Changed("port") {
			listenPort = port
		}

		// 1. Open SQLite database
		dbConn, err := db.Open(dbFile)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer func() { _ = dbConn.Close() }()

		// 2. Run migrations
		if migrationErr := db.RunMigrations(dbConn); migrationErr != nil {
			return fmt.Errorf("failed to run migrations: %w", migrationErr)
		}

		// Initialize Docker client
		dockerClient, dErr := docker.NewClient(cfg.Docker.Host)
		if dErr != nil {
			return fmt.Errorf("failed to initialize Docker client: %w", dErr)
		}

		// Sync containers immediately after migration at startup
		if syncErr := container.SyncContainers(cmd.Context(), dbConn, dockerClient); syncErr != nil {
			return fmt.Errorf("failed to sync containers at startup: %w", syncErr)
		}

		// 3. Initialize queries and services
		queries := db.New(dbConn)
		containerBroker := container.NewBroker()

		go docker.StartEventMonitor(cmd.Context(), logger.With("module", "docker"), queries, dockerClient, func(action string, containerID string) {
			switch action {
			case "save":
				dbQueries := db.New(dbConn)
				c, dbErr := dbQueries.GetContainer(context.Background(), containerID)
				if dbErr != nil {
					cmdLogger.Error("Failed to fetch container for stream event broadcast", "container_id", containerID, "error", dbErr)
					return
				}
				containerBroker.Publish(&dmanagerv1.StreamContainersResponse{
					Action:      "save",
					ContainerId: containerID,
					Container:   container.MapContainerRecord(c),
				})
			case "delete":
				containerBroker.Publish(&dmanagerv1.StreamContainersResponse{
					Action:      "delete",
					ContainerId: containerID,
				})
			}
		})

		authSvc := auth.NewService(queries, logger.With("module", "auth"))
		authInterceptor := auth.NewInterceptor(queries, logger.With("module", "auth"))

		// 4. Set up HTTP handler/mux
		mux := http.NewServeMux()

		// Register AuthService
		authPath, authHandler := dmanagerv1connect.NewAuthServiceHandler(
			authSvc,
			connect.WithInterceptors(authInterceptor),
		)
		mux.Handle(authPath, authHandler)

		// Register ContainerService
		containerSvc := container.NewService(dbConn, containerBroker, dockerClient, logger.With("module", "container"), cfg.Registries)
		containerPath, containerHandler := dmanagerv1connect.NewContainerServiceHandler(
			containerSvc,
			connect.WithInterceptors(authInterceptor),
		)
		mux.Handle(containerPath, containerHandler)

		// Register LogService
		loggingSvc := logging.NewService(logger.With("module", "logging"))
		loggingPath, loggingHandler := dmanagerv1connect.NewLogServiceHandler(
			loggingSvc,
			connect.WithInterceptors(authInterceptor),
		)
		mux.Handle(loggingPath, loggingHandler)

		// Start background registry update checker scheduler
		container.StartScheduler(cmd.Context(), containerSvc, cfg.Scheduler.IntervalMinutes)

		// Register Frontend SPA static files handler
		subFS, err := fs.Sub(FrontendDist, "frontend/dist")
		if err != nil {
			return fmt.Errorf("failed to get frontend build subdirectory: %w", err)
		}

		indexBytes, err := fs.ReadFile(subFS, "index.html")
		if err != nil {
			return fmt.Errorf("failed to read frontend index.html: %w", err)
		}

		fileServer := http.FileServer(http.FS(subFS))
		spaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Clean(r.URL.Path)

			// If file exists, serve it
			f, err := subFS.Open(strings.TrimPrefix(path, "/"))
			if err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}

			// Otherwise serve index.html (SPA routing fallback)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexBytes)
		})

		mux.Handle("/", spaHandler)

		// Apply CORS middleware
		handler := withCORS(cfg.Server.AllowedOrigins, mux)

		// 5. Create HTTP Server
		server := &http.Server{
			Addr:         ":" + listenPort,
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  30 * time.Second,
		}

		// 6. Graceful shutdown handler
		shutdownErr := make(chan error, 1)
		go func() {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			<-sigChan

			cmdLogger.Info("Shutting down backend server...")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			shutdownErr <- server.Shutdown(ctx)
		}()

		cmdLogger.Info("Starting dmanager server", "port", listenPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server listen and serve failed: %w", err)
		}

		if err := <-shutdownErr; err != nil {
			cmdLogger.Error("Graceful shutdown failed", "error", err)
		} else {
			cmdLogger.Info("Server stopped gracefully")
		}

		return nil
	},
}

func withCORS(allowedOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if len(allowedOrigins) > 0 && origin != "" {
			matched := false
			for _, allowed := range allowedOrigins {
				if allowed == "*" || allowed == origin {
					matched = true
					break
				}
			}
			if matched {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Connect-Protocol-Version, Connect-Timeout")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func init() {
	serveCmd.Flags().StringVarP(&configPath, "config", "c", "", "path to yaml configuration file")
	serveCmd.Flags().StringVarP(&port, "port", "p", "9283", "port to listen on")
	serveCmd.Flags().StringVarP(&dbPath, "db", "d", "dmanager.db", "path to sqlite database file")
	rootCmd.AddCommand(serveCmd)
}
