package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vancheszz/android-agent/internal/input"
	"github.com/Vancheszz/android-agent/internal/server"
)

func main() {
	log.Println("Starting Nidhogg...")

	drv, err := input.NewDriverAutoEV("/dev/graphics/fb0", 1080, 1920, 4)
	if err != nil {
		log.Fatalf("Failed to initialize driver: %v", err)
	}
	defer drv.Close()

	screenWidth, screenHeight := drv.GetScreenSize()
	touchX, touchY := drv.GetTouchLimits()
	log.Printf("Driver initialized: screen=%dx%d, touch_limits=(%d,%d), bpp=%d",
		screenWidth, screenHeight, touchX, touchY, drv.BytesPerPixel)

	srv := server.NewServer(drv)

	// Запускаем сервер для Ratatoskr (порт :9999)
	go func() {
		if err := srv.StartRatatoskrServer(":9999"); err != nil {
			log.Fatalf("Ratatoskr server failed: %v", err)
		}
	}()

	// Запускаем сервер для Yggdrasil (порт :9998)
	go func() {
		if err := srv.StartYggdrasilServer(":9998"); err != nil {
			log.Fatalf("Yggdrasil server failed: %v", err)
		}
	}()

	log.Println("Nidhogg is running")
	log.Println("  - Ratatoskr port: :9999 (receives ScreenDump)")
	log.Println("  - Yggdrasil port: :9998 (receives commands)")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Nidhogg...")
	srv.Stop()
	log.Println("Done")
}
