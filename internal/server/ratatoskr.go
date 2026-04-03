package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	pb "github.com/Vancheszz/android-agent/internal/ratatoskr"
	"google.golang.org/protobuf/proto"
)

type RatatoskrHandler struct {
	registry      *Registry
	lastDump      *pb.ScreenDump
	lastDumpMutex sync.RWMutex

	dumpRequestCh <-chan chan *pb.ScreenDump
}

func NewRatatoskrHandler(registry *Registry, dumpRequestCh <-chan chan *pb.ScreenDump) *RatatoskrHandler {
	return &RatatoskrHandler{
		registry:      registry,
		dumpRequestCh: dumpRequestCh,
	}
}

func (h *RatatoskrHandler) Handle(conn net.Conn) {
	defer conn.Close()
	fmt.Println("[Ratatoskr] Client connected")
	go h.handleDumpRequests()
	for {
		// Читаем заголовок (4 байта длины)
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			if err == io.EOF {
				fmt.Println("[Ratatoskr] Client disconnected")
			} else {
				fmt.Printf("[Ratatoskr] Header read error: %v\n", err)
			}
			return
		}

		size := binary.BigEndian.Uint32(header)

		// Читаем payload
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			fmt.Printf("[Ratatoskr] Payload read error: %v\n", err)
			return
		}

		// Парсим ScreenDump
		dump := &pb.ScreenDump{}
		if err := proto.Unmarshal(payload, dump); err != nil {
			fmt.Printf("[Ratatoskr] Protobuf unmarshal error: %v\n", err)
			continue
		}
		h.lastDumpMutex.Lock()
		h.lastDump = dump
		h.lastDumpMutex.Unlock()

		fmt.Printf("[Ratatoskr] Received dump: pkg=%s, nodes=%d, time=%d\n",
			dump.PackageName, len(dump.Nodes), dump.Timestamp)

		// Отправляем дамп в Yggdrasil (если подключён)
		if client := h.registry.Get(); client != nil {
			client.SendScreenDump(dump)
		}
	}
}
func (h *RatatoskrHandler) handleDumpRequests() {
	for respCh := range h.dumpRequestCh {
		h.lastDumpMutex.RLock()
		dump := h.lastDump
		h.lastDumpMutex.RUnlock()

		if dump == nil {
			respCh <- nil
		} else {
			// Отправляем копию, чтобы оригинал не изменился
			respCh <- proto.Clone(dump).(*pb.ScreenDump)
		}
	}
}
