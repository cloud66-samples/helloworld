package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-redis/redis"
)

type key int

const (
	requestIDKey key = 0
)

var (
	listenAddr string
	redisAddr  string
	healthy    int32
)

// this pushes new items onto a stack on a random cycle
func main() {
	flag.StringVar(&listenAddr, "binding", "0.0.0.0:5000", "Server listen address")
	flag.StringVar(&redisAddr, "redis", "redis:6379", "Redis address (not required)")
	flag.Parse()

	cancel := make(chan os.Signal)
	signal.Notify(cancel, os.Interrupt, syscall.SIGTERM)

	logger := log.New(os.Stdout, "http: ", log.LstdFlags)
	logger.Printf("Server is starting on %s...\n", listenAddr)
	logger.Printf("Checking Redis on %s...\n", redisAddr)

	router := http.NewServeMux()
	router.Handle("/style.css", http.FileServer(http.Dir("./static")))
	router.Handle("/background.jpg", http.FileServer(http.Dir("./static")))
	router.HandleFunc("/", handler)

	nextRequestID := func() string {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      tracing(nextRequestID)(logging(logger)(router)),
		ErrorLog:     logger,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	done := make(chan bool)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	go func() {
		<-quit
		logger.Println("Server is shutting down...")
		atomic.StoreInt32(&healthy, 0)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		server.SetKeepAlivesEnabled(false)
		if err := server.Shutdown(ctx); err != nil {
			logger.Fatalf("Could not gracefully shutdown the server: %v\n", err)
		}
		close(done)
	}()

	logger.Println("Server is ready to handle requests at", listenAddr)
	atomic.StoreInt32(&healthy, 1)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Could not listen on %s: %v\n", listenAddr, err)
	}

	<-done
	logger.Println("Server stopped")
}

func handler(w http.ResponseWriter, r *http.Request) {
	var contentBytes, _ = ioutil.ReadFile("./static/index.html")
	var content = string(contentBytes)
	var leadContent string
	if testRedisConnection(redisAddr) {
		leadContent = "This is a simple service application(connected to Redis). Deployed by Cloud 66 ~"
	} else {
		leadContent = "This is a simple single service application. Deployed by Cloud 66"
	}
	content = strings.Replace(content, "{{LEAD}}", leadContent, -1)
	w.Write([]byte(content))
}

func healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&healthy) == 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}

func testRedisConnection(redisAddress string) bool {
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	pong, _ := client.Ping().Result()
	if pong == "PONG" {
		return true
	}
	return false
	// Output: PONG <nil>
}

func logging(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				requestID, ok := r.Context().Value(requestIDKey).(string)
				if !ok {
					requestID = "unknown"
				}
				logger.Println(requestID, r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func tracing(nextRequestID func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-Id")
			if requestID == "" {
				requestID = nextRequestID()
			}
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			w.Header().Set("X-Request-Id", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
