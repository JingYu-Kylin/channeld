package channeld

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"channeld.clewcat.com/channeld/proto"
	"github.com/gorilla/websocket"
	protobuf "google.golang.org/protobuf/proto"
	nws "nhooyr.io/websocket"
)

func getBenchmarkBytes() []byte {
	bytes, _ := protobuf.Marshal(&proto.ChatMessage{
		Sender:   "guest12345",
		SendTime: time.Now().Unix(),
		Content:  "Mary had a little lamb",
	})
	return bytes
}

const writeTimes = 100000

var wg sync.WaitGroup

func TestGorillaWebSocket(t *testing.T) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			data := getBenchmarkBytes()

			startTime := time.Now()
			w, _ := conn.NextWriter(websocket.BinaryMessage)
			for i := 0; i < writeTimes; i++ {
				w.Write(data)
			}
			w.Close()
			t.Logf("Write %d times: %dms", writeTimes, time.Since(startTime).Milliseconds())

			conn.Close()
		} else {
			t.Errorf("failed to upgrade websocket connection: %v", err)
		}
		wg.Done()
	})
	wg.Add(1)
	go http.ListenAndServe("localhost:8081", nil)
	time.Sleep(time.Second)

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8081", nil)
	if err == nil {
		startTime := time.Now()
		_, bytes, err := conn.ReadMessage()
		if err == nil {
			t.Logf("read in %dms (%d bytes)\n", time.Since(startTime).Milliseconds(), len(bytes))
		} else {
			t.Errorf("error reading message: %v", err)
		}
	} else {
		t.Fatalf("failed dialing: %v", err)
	}

	wg.Wait()

	// Result: 10x FASTER than nhooyr/websocket!
	// write 10,000 times: 2ms; read in 3ms
	// write 100,000 times: 24-25ms; read in 24-25ms
}

func TestNhooyrWebSocket(t *testing.T) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := nws.Accept(w, r, nil)
		if err == nil {
			data := getBenchmarkBytes()
			ctx := r.Context()

			startTime := time.Now()
			w, _ := conn.Writer(ctx, nws.MessageBinary)
			for i := 0; i < writeTimes; i++ {
				w.Write(data)
			}
			w.Close()
			t.Logf("write %d times: %dms", writeTimes, time.Since(startTime).Milliseconds())

			conn.Close(nws.StatusNormalClosure, "finished benchmark")
		} else {
			t.Errorf("failed to upgrade websocket connection: %v", err)
		}
		wg.Done()
	})
	wg.Add(1)
	go http.ListenAndServe("localhost:8081", nil)
	time.Sleep(time.Second)

	clientCtx := context.Background()
	conn, _, err := nws.Dial(clientCtx, "ws://localhost:8081", nil)
	if err == nil {
		conn.SetReadLimit(0xffffff)
		startTime := time.Now()
		_, bytes, err := conn.Read(clientCtx)
		if err == nil {
			t.Logf("read in %dms (%d bytes)\n", time.Since(startTime).Milliseconds(), len(bytes))
		} else {
			t.Errorf("error reading message: %v", err)
		}
	} else {
		t.Fatalf("failed dialing: %v", err)
	}

	wg.Wait()

	// Result:
	// write 10,000 times: 19-22ms; read in 28-32ms
	// write 100,000 times: 197-215ms; read in 290-317ms
}
