package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"slices"
	"sync"
	"time"
)

const messagesToSend = 1000

type client struct {
	id      uint64
	msgLog  []string
	sentLog []string

	listen bool
	send   bool

	ctx    context.Context
	cancel context.CancelFunc
}

func main() {
	var globalLogWG sync.WaitGroup
	var clientWG sync.WaitGroup

	globalLogCtx, globalLogCancel := context.WithCancel(context.Background())
	globalLog := client{id: 0, listen: true, send: false, ctx: globalLogCtx, cancel: globalLogCancel}
	globalLogWG.Add(1)
	go func() {
		defer globalLogWG.Done()
		globalLog.start()
	}()

	var clients []*client
	for i := 1; i <= 80; i++ {
		clientCtx, clientCancel := context.WithCancel(context.Background())
		newClient := client{id: uint64(i), listen: true, send: true, ctx: clientCtx, cancel: clientCancel}
		clients = append(clients, &newClient)
		clientWG.Add(1)
		go func() {
			defer clientWG.Done()
			newClient.start()
		}()
	}

	clientWG.Wait()

	globalLog.cancel()
	globalLogWG.Wait()

	for _, client := range clients {
		currentGlobalIdx := 0
		for i := 0; i < len(client.msgLog); {
			msg := client.msgLog[i]
			globalMsg := globalLog.msgLog[currentGlobalIdx]

			if msg != globalMsg {
				if !slices.Contains(client.sentLog, globalMsg) {
					log.Fatalf("FATAL: messages are not the same for client %d and global message log.\n\nClient Message (idx %d): %v\n\nGlobal (idx %d):%v", client.id, i, msg, currentGlobalIdx, globalMsg)
				}
			} else {
				i++
			}

			currentGlobalIdx++
		}
	}

	log.Printf("\n\nSUCCESS:Test passed!\n")
}

func (c *client) start() {
	conn, err := net.Dial("tcp", ":8000")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("INFO: Started client %d\n", c.id)

	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		conn.Close()
	}()

	time.Sleep(1 * time.Second)

	if c.send {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.startSend(conn)
		}()
	}

	if c.listen {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.startListen(conn)
		}()
	}

	<-c.ctx.Done()
}

func (c *client) startSend(conn net.Conn) {
	messagesSent := 0

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("INFO: Cancel received, ending sending client. (Client %d)\n", c.id)
		default:
		}

		msg := fmt.Sprintf("Good news, everyone! Client %d is here! This is message no. %d from me!\n", c.id, messagesSent)
		_, err := fmt.Fprintf(conn, msg)

		if err != nil {
			log.Printf("ERROR: during sending (Client %d): %v\n", c.id, err)
			c.cancel()
			return
		}

		c.sentLog = append(c.sentLog, msg)
		messagesSent++
		time.Sleep(time.Duration(rand.Int()%25) * time.Millisecond)

		if messagesSent >= messagesToSend {
			log.Printf("INFO: Messages sent, exiting sending client. (Client %d)\n", c.id)
			return
		}
	}
}

func (c *client) startListen(conn net.Conn) {
	reader := bufio.NewReader(conn)
	messagesReceived := 0

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("INFO: Cancel received, ending listening client. (Client %d, %d received)\n", c.id, messagesReceived)
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(3000 * time.Millisecond))
		msg, err := reader.ReadString('\n')

		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				log.Printf("INFO: Stopped listening due to timeout. (Client %d)\n", c.id)
			} else if err == io.EOF {
				log.Printf("INFO: stopped listening due to connection closing. (Client %d)\n", c.id)
			} else {
				log.Printf("ERROR: during listening (Client %d): %v\n", c.id, err)
			}
			c.cancel()
		} else {
			messagesReceived++
			c.msgLog = append(c.msgLog, msg)
		}
	}
}
