package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	connect "connectrpc.com/connect"
	"github.com/spf13/cobra"

	"dmanager/internal/auth"
	"dmanager/internal/config"
	"dmanager/internal/db"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
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
		if err := db.RunMigrations(dbConn); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// 3. Initialize queries and services
		queries := db.New(dbConn)
		authSvc := auth.NewService(queries)
		authInterceptor := auth.NewInterceptor(queries)

		// 4. Set up HTTP handler/mux
		mux := http.NewServeMux()

		// Register AuthService
		authPath, authHandler := dmanagerv1connect.NewAuthServiceHandler(
			authSvc,
			connect.WithInterceptors(authInterceptor),
		)
		mux.Handle(authPath, authHandler)

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

			log.Println("Shutting down backend server...")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			shutdownErr <- server.Shutdown(ctx)
		}()

		log.Printf("Starting dmanager server on port %s...", listenPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server listen and serve failed: %w", err)
		}

		if err := <-shutdownErr; err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
		} else {
			log.Println("Server stopped gracefully")
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
	serveCmd.Flags().StringVarP(&port, "port", "p", "8080", "port to listen on")
	serveCmd.Flags().StringVarP(&dbPath, "db", "d", "dmanager.db", "path to sqlite database file")
	rootCmd.AddCommand(serveCmd)
}
