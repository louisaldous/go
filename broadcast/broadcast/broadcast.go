package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var nextConnectionId atomic.Uint64

type client struct {
	id        uint64
	inputChan chan []byte
	conn      net.Conn

	ctx    context.Context
	cancel context.CancelFunc
}

type message struct {
	data   []byte
	sender uint64 // client id
}

func (c *client) start(broadcastChan chan<- message, unregisterChan chan<- uint64) {
	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		unregisterChan <- c.id
		close(c.inputChan)
		c.conn.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.listen(broadcastChan)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.send()
	}()

	<-c.ctx.Done()
}

func (c *client) listen(broadcastChan chan<- message) {
	reader := bufio.NewReader(c.conn)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
		msg, err := reader.ReadBytes('\n')
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				if err == io.EOF {
					log.Printf("INFO: Connection to %d closed (EOF).\n", c.id)
				} else {
					log.Printf("WARNING: Error reading from client %d: %v\n", c.id, err)
				}

				c.cancel()
				return
			}
		}

		if len(msg) > 0 {
			broadcastChan <- message{data: msg, sender: c.id}
		}
	}
}

func (c *client) send() {
	for {
		select {
		case <-c.ctx.Done():
			return

		case msg, ok := <-c.inputChan:
			if !ok {
				return
			}

			_, err := c.conn.Write(msg)
			if err != nil {
				log.Printf("WARNING: error writing to client: %v\n", err)
				c.cancel()
				return
			}
		}

	}
}

func main() {
	listener, err := net.Listen("tcp", ":8000")
	if err != nil {
		log.Fatal(err)
	}

	broadcastChan := make(chan message)
	registerChan := make(chan *client)
	unregisterChan := make(chan uint64)

	go broadcast(registerChan, unregisterChan, broadcastChan)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}

		clientCtx, cancel := context.WithCancel(context.Background())

		newClient := client{
			id:        nextConnectionId.Add(1),
			inputChan: make(chan []byte, 100),
			conn:      conn,
			ctx:       clientCtx,
			cancel:    cancel,
		}

		registerChan <- &newClient
		go newClient.start(broadcastChan, unregisterChan)
	}
}

func broadcast(registerChan <-chan *client, unregisterChan <-chan uint64, broadcastChan <-chan message) {
	connections := make(map[uint64]*client)
	log.Println("INFO: broadcast function ready and waiting")

	for {
		select {
		case msg := <-broadcastChan:
			for _, client := range connections {
				if msg.sender != client.id {
					select {
					case client.inputChan <- msg.data:
					}
				}
			}

		case newClient := <-registerChan:
			connections[newClient.id] = newClient
			log.Printf("INFO: Client %d registered.\n", newClient.id)

		case clientID := <-unregisterChan:
			if clientPtr, ok := connections[clientID]; ok {
				clientPtr.cancel()
				delete(connections, clientID)
				log.Printf("INFO: Client %d unregistered.\n", clientID)
			}
		}
	}
}
