package main

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"sync/atomic"
)

var nextConnectionId atomic.Uint64

type client struct {
	clientID           uint64
	clientInputChannel chan []byte
	tcpConnection      net.Conn

	ctx    context.Context
	cancel context.CancelFunc
}

type message struct {
	data   []byte
	sender uint64 // client id
}

func (c *client) start(broadcastChan chan<- message, unregisterClientChan chan<- uint64) {
	defer func() {
		unregisterClientChan <- c.clientID
		close(c.clientInputChannel)
		c.tcpConnection.Close()
	}()

	go c.listen(broadcastChan)
	go c.send()

	<-c.ctx.Done()
}

func (c *client) listen(broadcastChan chan<- message) {
	reader := bufio.NewReader(c.tcpConnection)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		msg, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				log.Printf("INFO: Connection to %d closed (EOF).\n", c.clientID)
			} else {
				log.Printf("WARNING: Error reading from client %d: %v\n", c.clientID, err)
			}
			c.cancel()
			return
		}

		if len(msg) > 0 {
			broadcastChan <- message{data: msg, sender: c.clientID}
		}
	}
}

func (c *client) send() {
	for {
		select {
		case <-c.ctx.Done():
			return

		case msg, ok := <-c.clientInputChannel:
			if !ok {
				return
			}

			_, err := c.tcpConnection.Write(msg)
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
	registerClientChan := make(chan *client)
	unregisterClientChan := make(chan uint64)

	go broadcast(registerClientChan, unregisterClientChan, broadcastChan)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}

		clientCtx, cancel := context.WithCancel(context.Background())

		newClient := client{
			clientID:           nextConnectionId.Add(1),
			clientInputChannel: make(chan []byte, 100),
			tcpConnection:      conn,
			ctx:                clientCtx,
			cancel:             cancel,
		}

		registerClientChan <- &newClient
		go newClient.start(broadcastChan, unregisterClientChan)
	}
}

func broadcast(registerClientChan <-chan *client, unregisterClientChan <-chan uint64, broadcastChan <-chan message) {
	connections := make(map[uint64]*client)
	log.Println("INFO: broadcast function ready and waiting")

	for {
		select {
		case msg := <-broadcastChan:
			for _, client := range connections {
				select {
				case client.clientInputChannel <- msg.data:
				}
			}

		case newClient := <-registerClientChan:
			connections[newClient.clientID] = newClient
			log.Printf("INFO: Client %d registered.\n", newClient.clientID)

		case clientID := <-unregisterClientChan:
			if clientPtr, ok := connections[clientID]; ok {
				clientPtr.cancel()
				delete(connections, clientID)
				log.Printf("INFO: Client %d unregistered.\n", clientID)
			}
		}
	}
}
