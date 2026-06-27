package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	agileconfig "github.com/animacaeli/go-agile-config"
)

type settings struct {
	ServerURL    string
	AppID        string
	Secret       string
	Env          string
	Listen       string
	InsecureHTTP bool
}

func main() {
	cfg := parseSettings()
	if cfg.ServerURL == "" || cfg.AppID == "" {
		log.Fatal("AGILE_CONFIG_SERVER and AGILE_CONFIG_APP_ID are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var client *agileconfig.Client
	var ready atomic.Bool
	clientOpts := []agileconfig.Option{
		agileconfig.WithEnv(cfg.Env),
		agileconfig.WithWSPingInterval(5 * time.Second),
		agileconfig.WithOnChange(func(changedKeys []string) {
			if !ready.Load() {
				return
			}
			sort.Strings(changedKeys)
			log.Printf("agileconfig changed keys: %s", strings.Join(changedKeys, ", "))
			printConfigs("current configs after change", client.GetAll())
		}),
	}
	if cfg.InsecureHTTP {
		clientOpts = append(clientOpts, agileconfig.WithInsecureHTTP())
	}
	client = agileconfig.NewClient(cfg.ServerURL, cfg.AppID, cfg.Secret, clientOpts...)

	if err := client.Start(ctx); err != nil {
		log.Fatalf("start agileconfig client: %v", err)
	}
	defer client.Stop()

	printConfigs("initial configs", client.GetAll())
	ready.Store(true)

	router := gin.Default()
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/configs", func(c *gin.Context) {
		c.JSON(http.StatusOK, client.GetAll())
	})
	router.GET("/configs/:key", func(c *gin.Context) {
		key := c.Param("key")
		value, ok := client.Get(key)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "config not found", "key": key})
			return
		}
		c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
	})
	router.GET("/groups/:group/configs/:key", func(c *gin.Context) {
		group := c.Param("group")
		key := c.Param("key")
		value, ok := client.GetByGroup(group, key)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "config not found", "group": group, "key": key})
			return
		}
		c.JSON(http.StatusOK, gin.H{"group": group, "key": key, "value": value})
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("gin demo listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("gin server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("gin server shutdown: %v", err)
	}
}

func parseSettings() settings {
	cfg := settings{
		ServerURL:    env("AGILE_CONFIG_SERVER", "https://localhost:5000"),
		AppID:        env("AGILE_CONFIG_APP_ID", ""),
		Secret:       env("AGILE_CONFIG_SECRET", ""),
		Env:          env("AGILE_CONFIG_ENV", ""),
		Listen:       env("GIN_LISTEN", ":8080"),
		InsecureHTTP: envBool("AGILE_CONFIG_INSECURE_HTTP"),
	}

	flag.StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "AgileConfig server URL")
	flag.StringVar(&cfg.AppID, "app-id", cfg.AppID, "AgileConfig app ID")
	flag.StringVar(&cfg.Secret, "secret", cfg.Secret, "AgileConfig app secret")
	flag.StringVar(&cfg.Env, "env", cfg.Env, "AgileConfig environment")
	flag.StringVar(&cfg.Listen, "listen", cfg.Listen, "Gin listen address")
	flag.BoolVar(&cfg.InsecureHTTP, "insecure-http", cfg.InsecureHTTP, "allow http:// AgileConfig server URLs for local development")
	flag.Parse()

	return cfg
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func printConfigs(title string, configs map[string]string) {
	keys := make([]string, 0, len(configs))
	for key := range configs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fmt.Printf("\n[%s] total=%d\n", title, len(keys))
	for _, key := range keys {
		value, err := json.Marshal(configs[key])
		if err != nil {
			fmt.Printf("%s = %s\n", key, configs[key])
			continue
		}
		fmt.Printf("%s = %s\n", key, value)
	}
}
