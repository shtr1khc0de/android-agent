package server

import (
	"net"

	"github.com/Vancheszz/android-agent/internal/input"
	pb "github.com/Vancheszz/android-agent/internal/ratatoskr"
)

type Server struct {
	driver   *input.Driver
	registry *Registry

	ratatoskrListener net.Listener
	yggdrasilListener net.Listener

	// Канал для передачи запросов дампа от Yggdrasil к Ratatoskr
	dumpRequestCh chan chan *pb.ScreenDump
}

func NewServer(driver *input.Driver) *Server {
	dumpRequestCh := make(chan chan *pb.ScreenDump)

	return &Server{
		driver:        driver,
		registry:      NewRegistry(),
		dumpRequestCh: dumpRequestCh,
	}
}

func (s *Server) StartRatatoskrServer(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ratatoskrListener = listener

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Если листенер закрыт, выходим
			return nil
		}
		handler := NewRatatoskrHandler(s.registry, s.dumpRequestCh)
		go handler.Handle(conn)
	}
}

func (s *Server) StartYggdrasilServer(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.yggdrasilListener = listener

	for {
		conn, err := listener.Accept()
		if err != nil {
			return nil
		}
		handler := NewYggdrasilHandler(conn, s.driver, s.registry)
		// Передаём канал для запросов дампа
		handler.dumpRequestCh = s.dumpRequestCh
		go handler.Handle()
	}
}

func (s *Server) Stop() {
	if s.ratatoskrListener != nil {
		s.ratatoskrListener.Close()
	}
	if s.yggdrasilListener != nil {
		s.yggdrasilListener.Close()
	}
}
