package api

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joekhosbayar/go-mighty/internal/service"
)

// TestWSHandler_RejectEarlyData confirms that the server rejects a WebSocket handshake
// if the client sends data before the handshake is complete.
func TestWSHandler_RejectEarlyData(t *testing.T) {
	// Setup minimal dependencies
	svc := service.NewGameService(nil, nil)
	handler := NewHandler(svc)

	server := httptest.NewServer(http.HandlerFunc(handler.WSHandler))
	defer server.Close()

	// Parse the URL to get the host/port
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to split host/port: %v", err)
	}

	// Connect with raw TCP
	conn, err := net.Dial("tcp", "localhost:"+port)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// 1. Send HTTP Upgrade Request
	// Note: We intentionally do NOT wait for the response here.
	req := "GET /games/test-game/ws HTTP/1.1\r\n" +
		"Host: localhost:" + port + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: http://localhost:" + port + "\r\n" +
		"\r\n"

	// 2. Immediately send some "garbage" or valid frame data
	// This simulates the client sending data before the server has completely processed the handshake
	extraData := []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}

	// Write everything at once
	_, err = conn.Write([]byte(req))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}
	_, err = conn.Write(extraData)
	if err != nil {
		t.Fatalf("Failed to write extra data: %v", err)
	}

	// 3. Read the response from the server
	// We expect the connection to be closed or a failure response.
	// The server logs "websocket: client sent data before handshake is complete"
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err == nil {
		if resp.StatusCode == http.StatusSwitchingProtocols {
			t.Errorf("Expected handshake failure, got 101 Switching Protocols")
		}
		resp.Body.Close()
	} else {
		// Error is expected (e.g. EOF if server closes conn)
		t.Logf("Got expected error/closure: %v", err)
	}
}
