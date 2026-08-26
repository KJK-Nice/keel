package session

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"

	"keel/internal/store"
)

type Bridge interface {
	io.ReadWriter
	Resize(cols, rows int) error
	Close() error
}

type controlMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type wsConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsConn) writeJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *wsConn) writeBinary(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, p)
}

func Open(host store.Host, cols, rows int, checkKey func(hostname, fingerprint string) error) (Bridge, error) {
	if host.AuthType == "local" {
		return openLocal(cols, rows)
	}
	return openSSH(host, cols, rows, checkKey)
}

func BridgeWS(conn *websocket.Conn, br Bridge) {
	ws := &wsConn{conn: conn}
	defer func() {
		_ = br.Close()
		_ = conn.Close()
	}()

	errc := make(chan error, 2)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if werr := ws.writeBinary(buf[:n]); werr != nil {
					errc <- werr
					return
				}
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if mt == websocket.TextMessage {
				var ctl controlMessage
				if err := json.Unmarshal(msg, &ctl); err != nil {
					continue
				}
				if ctl.Type == "resize" {
					if rerr := br.Resize(ctl.Cols, ctl.Rows); rerr != nil {
						log.Printf("resize: %v", rerr)
					}
				}
				continue
			}
			if _, err := br.Write(msg); err != nil {
				errc <- err
				return
			}
		}
	}()

	err := <-errc
	if err != nil && err != io.EOF {
		_ = ws.writeJSON(map[string]string{
			"type":    "error",
			"message": fmt.Sprintf("session ended: %v", err),
		})
	}
}

func ParseSize(colsQ, rowsQ string) (int, int) {
	cols, _ := strconv.Atoi(colsQ)
	rows, _ := strconv.Atoi(rowsQ)
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return cols, rows
}
