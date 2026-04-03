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
	conn          net.Conn // Сохраняем соединение для отправки команд
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

	// Запускаем обработку запросов дампа
	go h.handleDumpRequests()

	// Цикл чтения дампов от Ratatoskr
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

		// Сохраняем последний дамп
		h.lastDumpMutex.Lock()
		h.lastDump = dump
		h.lastDumpMutex.Unlock()

		fmt.Printf("[Ratatoskr] Received dump: pkg=%s, nodes=%d, time=%d\n",
			dump.PackageName, len(dump.Nodes), dump.Timestamp)

		// Отправляем дамп в Yggdrasil (если подключён)
		if client := h.registry.GetYggdrasil(); client != nil {
			client.SendScreenDump(dump)
		}
	}
}

// Обработка запросов на получение дампа от Yggdrasil
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

// SendCommand отправляет команду в Ratatoskr (через то же соединение)
func (h *RatatoskrHandler) SendCommand(cmd *pb.AgentCMD) error {
	if h.conn == nil {
		return fmt.Errorf("no connection to Ratatoskr")
	}

	// Сериализуем команду
	data, err := proto.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	// Отправляем с фреймингом (4 байта длины, BigEndian)
	if err := binary.Write(h.conn, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write length error: %w", err)
	}
	if _, err := h.conn.Write(data); err != nil {
		return fmt.Errorf("write data error: %w", err)
	}

	fmt.Printf("[Ratatoskr] Sent command: type=%v, payload=%s\n", cmd.Type, cmd.Payload)
	return nil
}
