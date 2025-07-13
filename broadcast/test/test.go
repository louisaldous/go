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
	"reflect"
	"sync"
	"time"
)

const messagesToSend = 1000

type client struct {
	clientID uint64
	msgLog   []string
	ctx      context.Context
	cancel   context.CancelFunc
}

func main() {
	var wg sync.WaitGroup

	var clients []*client
	for i := range 80 {
		clientCtx, clientCancel := context.WithCancel(context.Background())
		newClient := client{clientID: uint64(i), ctx: clientCtx, cancel: clientCancel}
		clients = append(clients, &newClient)
		wg.Add(1)
		go func() {
			defer wg.Done()
			newClient.start()
		}()
	}

	wg.Wait()

	firstClient := clients[0]

	for _, thisClient := range clients {
		if !reflect.DeepEqual(firstClient.msgLog, thisClient.msgLog) {
			log.Printf("Not equal!")
			log.Printf("First client: %v\n", firstClient.msgLog)
			log.Printf("This client: %v\n", thisClient.msgLog)
			return
		}
	}

	log.Printf("Test passed!\n")
}

func (c *client) start() {
	conn, err := net.Dial("tcp", ":8000")
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		conn.Close()
	}()

	time.Sleep(1 * time.Second)

	wg.Add(2)

	go func() {
		defer wg.Done()
		c.startSend(conn)
	}()

	go func() {
		defer wg.Done()
		c.startListen(conn)
	}()

	<-c.ctx.Done()
}

func (c *client) startSend(conn net.Conn) {
	messagesSent := 0

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("INFO: Cancel received, ending sending client. (Client %d)\n", c.clientID)
		default:
		}

		msg := fmt.Sprintf("Good news, everyone! Client %d is here! This is message no. %d from me!\n", c.clientID, messagesSent)
		_, err := fmt.Fprintf(conn, msg)

		if err != nil {
			log.Printf("ERROR: during sending (Client %d): %v\n", c.clientID, err)
			c.cancel()
			return
		}

		messagesSent++
		time.Sleep(time.Duration(rand.Int()%10) * time.Millisecond)

		if messagesSent >= messagesToSend {
			log.Printf("INFO: Messages sent, exiting sending client. (Client %d)\n", c.clientID)
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
			log.Printf("INFO: Cancel received, ending listening client. (Client %d, %d received)\n", c.clientID, messagesReceived)
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
		msg, err := reader.ReadString('\n')

		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				log.Printf("INFO: Stopped listening due to timeout. (Client %d)\n", c.clientID)
			} else if err == io.EOF {
				log.Printf("INFO: stopped listening due to connection closing. (Client %d)\n", c.clientID)
			} else {
				log.Printf("ERROR: during listening (Client %d): %v\n", c.clientID, err)
			}
			c.cancel()
		} else {
			messagesReceived++
			c.msgLog = append(c.msgLog, msg)
		}
	}
}
