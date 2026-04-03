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

	drv, err := input.NewDriverAutoEV("/dev/graphics/fb0", 720, 1280, 4)
	if err != nil {
		log.Printf("Warning: Driver init failed: %v (crop will not work)", err)
	} else {
		defer drv.Close()
		screensize1, screensize2 := drv.GetScreenSize()
		touchimits1, touchlimits2 := drv.GetTouchLimits()

		log.Printf("Driver initialized: screen=%dx%d, touch_limits=(%d,%d), bpp=%d",
			screensize1, screensize2, touchimits1, touchlimits2, drv.GetBytesPerPixel())
	}

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
	log.Println("  - Ratatoskr port: :9999 (receives ScreenDump, sends commands)")
	log.Println("  - Yggdrasil port: :9998 (receives commands)")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Nidhogg...")
	srv.Stop()
	log.Println("Done")
}
