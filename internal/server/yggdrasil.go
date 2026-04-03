package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	control "github.com/Vancheszz/android-agent/internal/control"
	"github.com/Vancheszz/android-agent/internal/input"
	pb "github.com/Vancheszz/android-agent/internal/ratatoskr"
	"google.golang.org/protobuf/proto"
)

type YggdrasilHandler struct {
	conn          net.Conn
	driver        *input.Driver
	registry      *Registry
	dumpRequestCh chan<- chan *pb.ScreenDump
}

func NewYggdrasilHandler(conn net.Conn, driver *input.Driver, registry *Registry) *YggdrasilHandler {
	return &YggdrasilHandler{
		conn:     conn,
		driver:   driver,
		registry: registry,
	}
}

func (h *YggdrasilHandler) Handle() {
	defer h.conn.Close()
	defer h.registry.ClearYggdrasil()

	fmt.Println("[Yggdrasil] Client connected")

	// 1. Читаем NidhoggHello
	var msgLen uint32
	if err := binary.Read(h.conn, binary.BigEndian, &msgLen); err != nil {
		fmt.Printf("[Yggdrasil] Failed to read hello length: %v\n", err)
		return
	}

	helloData := make([]byte, msgLen)
	if _, err := io.ReadFull(h.conn, helloData); err != nil {
		fmt.Printf("[Yggdrasil] Failed to read hello: %v\n", err)
		return
	}

	hello := &control.NidhoggHello{}
	if err := proto.Unmarshal(helloData, hello); err != nil {
		fmt.Printf("[Yggdrasil] Failed to parse hello: %v\n", err)
		return
	}

	fmt.Printf("[Yggdrasil] Hello received: container_id=%s, screen=%dx%d, version=%s\n",
		hello.ContainerId, hello.ScreenWidth, hello.ScreenHeight, hello.Version)

	// 2. Регистрируем себя как активного клиента
	h.registry.SetYggdrasil(h)

	// 3. Цикл обработки команд
	for {
		var cmdLen uint32
		if err := binary.Read(h.conn, binary.BigEndian, &cmdLen); err != nil {
			if err != io.EOF {
				fmt.Printf("[Yggdrasil] Command read error: %v\n", err)
			}
			break
		}

		cmdData := make([]byte, cmdLen)
		if _, err := io.ReadFull(h.conn, cmdData); err != nil {
			fmt.Printf("[Yggdrasil] Failed to read command: %v\n", err)
			break
		}

		req := &control.NidhoggRequest{}
		if err := proto.Unmarshal(cmdData, req); err != nil {
			fmt.Printf("[Yggdrasil] Failed to parse command: %v\n", err)
			h.sendError("Invalid protobuf")
			continue
		}

		h.handleRequest(req)
	}
}

func (h *YggdrasilHandler) handleRequest(req *control.NidhoggRequest) {
	var resp *control.NidhoggResponse

	switch payload := req.Payload.(type) {
	case *control.NidhoggRequest_Tap:
		resp = h.handleTap(payload.Tap)

	//case *control.NidhoggRequest_Swipe:
	//	resp = h.handleSwipe(payload.Swipe)

	case *control.NidhoggRequest_Crop:
		resp = h.handleCrop(payload.Crop)

	case *control.NidhoggRequest_GetDump:
		resp = h.handleGetDump()

	default:
		resp = &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: "unknown command",
		}
	}

	h.sendResponse(resp)
}

func (h *YggdrasilHandler) handleTap(cmd *control.TapCommand) *control.NidhoggResponse {
	// Получаем Ratatoskr хендлер
	ratatoskr := h.registry.GetRatatoskr()
	if ratatoskr == nil {
		return &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: "Ratatoskr not connected",
		}
	}

	// Отправляем команду в Ratatoskr
	agentCmd := &pb.AgentCMD{
		Type:    pb.AgentCMD_CLICK,
		Payload: fmt.Sprintf("%d,%d", cmd.X, cmd.Y),
	}

	if err := ratatoskr.SendCommand(agentCmd); err != nil {
		return &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: fmt.Sprintf("Failed to send command: %v", err),
		}
	}

	return &control.NidhoggResponse{Success: true}
}

/*
	func (h *YggdrasilHandler) handleSwipe(cmd *control.SwipeCommand) *control.NidhoggResponse {
		// Получаем Ratatoskr хендлер
		ratatoskr := h.registry.GetRatatoskr()
		if ratatoskr == nil {
			return &control.NidhoggResponse{
				Success:  false,
				ErrorMsg: "Ratatoskr not connected",
			}
		}

		// Отправляем команду в Ratatoskr (свайп)
		agentCmd := &pb.AgentCMD{
			Type:    pb.AgentCMD_SWIPE,
			Payload: fmt.Sprintf("%d,%d,%d,%d,%d", cmd.X1, cmd.Y1, cmd.X2, cmd.Y2, cmd.DurationMs),
		}

		if err := ratatoskr.SendCommand(agentCmd); err != nil {
			return &control.NidhoggResponse{
				Success:  false,
				ErrorMsg: fmt.Sprintf("Failed to send swipe command: %v", err),
			}
		}

		return &control.NidhoggResponse{Success: true}
	}
*/
func (h *YggdrasilHandler) handleCrop(cmd *control.CropRequest) *control.NidhoggResponse {
	if h.driver == nil {
		return &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: "driver not initialized",
		}
	}

	imgData, err := h.driver.Crop(cmd.X, cmd.Y, cmd.Width, cmd.Height)
	if err != nil {
		return &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: err.Error(),
		}
	}

	return &control.NidhoggResponse{
		Success: true,
		Data:    &control.NidhoggResponse_ImageData{ImageData: imgData},
	}
}

func (h *YggdrasilHandler) handleGetDump() *control.NidhoggResponse {
	// Создаём канал для ответа
	respCh := make(chan *pb.ScreenDump, 1)

	// Отправляем запрос в Ratatoskr хендлер
	select {
	case h.dumpRequestCh <- respCh:
		// Ждём ответ (с таймаутом 5 секунд)
		select {
		case dump := <-respCh:
			if dump == nil {
				return &control.NidhoggResponse{
					Success:  false,
					ErrorMsg: "no dump available",
				}
			}
			// Конвертируем pb.ScreenDump → control.ScreenDump
			controlDump := convertScreenDump(dump)
			return &control.NidhoggResponse{
				Success: true,
				Data:    &control.NidhoggResponse_ScreenDump{ScreenDump: controlDump},
			}
		case <-time.After(5 * time.Second):
			return &control.NidhoggResponse{
				Success:  false,
				ErrorMsg: "timeout waiting for screen dump",
			}
		}
	case <-time.After(1 * time.Second):
		return &control.NidhoggResponse{
			Success:  false,
			ErrorMsg: "ratatoskr not ready",
		}
	}
}

// SendScreenDump отправляет асинхронный дамп в Yggdrasil
func (h *YggdrasilHandler) SendScreenDump(dump *pb.ScreenDump) {
	data, err := proto.Marshal(dump)
	if err != nil {
		fmt.Printf("[Yggdrasil] Failed to marshal ScreenDump: %v\n", err)
		return
	}

	if err := binary.Write(h.conn, binary.BigEndian, uint32(len(data))); err != nil {
		fmt.Printf("[Yggdrasil] Failed to send dump length: %v\n", err)
		return
	}

	if _, err := h.conn.Write(data); err != nil {
		fmt.Printf("[Yggdrasil] Failed to send dump: %v\n", err)
	}
}

func (h *YggdrasilHandler) sendResponse(resp *control.NidhoggResponse) {
	data, err := proto.Marshal(resp)
	if err != nil {
		fmt.Printf("[Yggdrasil] Failed to marshal response: %v\n", err)
		return
	}

	if err := binary.Write(h.conn, binary.BigEndian, uint32(len(data))); err != nil {
		fmt.Printf("[Yggdrasil] Failed to send response length: %v\n", err)
		return
	}

	if _, err := h.conn.Write(data); err != nil {
		fmt.Printf("[Yggdrasil] Failed to send response: %v\n", err)
	}
}

func (h *YggdrasilHandler) sendError(msg string) {
	h.sendResponse(&control.NidhoggResponse{
		Success:  false,
		ErrorMsg: msg,
	})
}

// Конвертация pb.ScreenDump → control.ScreenDump
func convertScreenDump(src *pb.ScreenDump) *control.ScreenDump {
	if src == nil {
		return nil
	}

	nodes := make([]*control.UiNode, len(src.Nodes))
	for i, node := range src.Nodes {
		nodes[i] = &control.UiNode{
			Id:          node.Id,
			ParentId:    node.ParentId,
			Text:        node.Text,
			ResourceId:  node.ResourceId,
			ClassName:   node.ClassName,
			IsClickable: node.IsClickable,
			Bounds: &control.Rect{
				Left:   node.Bounds.Left,
				Right:  node.Bounds.Right,
				Top:    node.Bounds.Top,
				Bottom: node.Bounds.Bottom,
			},
		}
	}

	return &control.ScreenDump{
		PackageName: src.PackageName,
		Timestamp:   src.Timestamp,
		Width:       src.Width,
		Height:      src.Height,
		Nodes:       nodes,
	}
}
