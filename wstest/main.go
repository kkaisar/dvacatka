package main

import (
	"fmt"
	"os"
	"time"

	"github.com/fasthttp/websocket"
)

// Тестовый ws-клиент: подключается к комнате лобби и печатает входящие сообщения.
func main() {
	url := os.Args[1] // ws://localhost:8080/ws/lobby/<id>
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		fmt.Println("DIAL ERROR:", err)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Println("CONNECTED")

	conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("READ END:", err)
			return
		}
		fmt.Println("RECV:", string(msg))
	}
}
