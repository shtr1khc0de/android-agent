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
	dumpRequestCh <-chan chan *pb.ScreenDump
	lastDump      *pb.ScreenDump
	lastDumpMutex sync.RWMutex
	conn          net.Conn
}

func NewRatatoskrHandler(registry *Registry, dumpRequestCh <-chan chan *pb.ScreenDump) *RatatoskrHandler {
	return &RatatoskrHandler{
		registry:      registry,
		dumpRequestCh: dumpRequestCh,
	}
}

func (h *RatatoskrHandler) Handle(conn net.Conn) {
	defer conn.Close()

	h.conn = conn
	h.registry.SetRatatoskr(h)
	defer h.registry.ClearRatatoskr()

	fmt.Println("[Ratatoskr] Client connected")

	// Запускаем обработку запросов дампа от Yggdrasil
	go h.handleDumpRequests()

	// Цикл чтения дампов от Ratatoskr
	for {
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
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			fmt.Printf("[Ratatoskr] Payload read error: %v\n", err)
			return
		}

		dump := &pb.ScreenDump{}
		if err := proto.Unmarshal(payload, dump); err != nil {
			fmt.Printf("[Ratatoskr] Protobuf unmarshal error: %v\n", err)
			continue
		}

		// Сохраняем последний дамп
		h.lastDumpMutex.Lock()
		h.lastDump = dump
		h.lastDumpMutex.Unlock()

		fmt.Printf("[Ratatoskr] Received dump: pkg=%s, nodes=%d\n",
			dump.PackageName, len(dump.Nodes))

		// Отправляем дамп в Yggdrasil (если подключён)
		if client := h.registry.GetYggdrasil(); client != nil {
			client.SendScreenDump(dump)
		}
	}
}

// handleDumpRequests обрабатывает запросы от Yggdrasil на получение свежего дампа
func (h *RatatoskrHandler) handleDumpRequests() {
	for respCh := range h.dumpRequestCh {
		h.lastDumpMutex.RLock()
		dump := h.lastDump
		h.lastDumpMutex.RUnlock()

		if dump == nil {
			fmt.Println("[Ratatoskr] GetDump: no dump available")
			respCh <- nil
		} else {
			fmt.Printf("[Ratatoskr] GetDump: returning dump with %d nodes\n", len(dump.Nodes))
			// Отправляем копию
			respCh <- proto.Clone(dump).(*pb.ScreenDump)
		}
	}
}

// SendCommand отправляет команду в Ratatoskr
func (h *RatatoskrHandler) SendCommand(cmd *pb.AgentCMD) error {
	if h.conn == nil {
		return fmt.Errorf("no connection to Ratatoskr")
	}

	data, err := proto.Marshal(cmd)
	if err != nil {
		return err
	}

	if err := binary.Write(h.conn, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	if _, err := h.conn.Write(data); err != nil {
		return err
	}

	fmt.Printf("[Ratatoskr] Sent command: %v\n", cmd.Type)
	return nil
}
