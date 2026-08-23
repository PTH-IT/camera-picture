// Command api là backend của app.
//
// Phạm vi (xem docs/adr/0001-capture-strategy.md): backend KHÔNG làm capture.
// Capture chạy trên điện thoại qua CascableCore. Backend giữ tài khoản, đồng bộ,
// render RAW khi xuất, điều phối AI, phân phối preset, và lưu trữ dài hạn.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Không đặt WriteTimeout ở đây: upload RAW là 50-60MB mỗi file và có thể
		// rất chậm qua mạng di động. Timeout đặt theo từng handler thay vì toàn cục.
	}

	go func() {
		log.Info("api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown failed", "err", err)
	}
	log.Info("stopped")
}
