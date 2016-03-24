package main

import (
	"bufio"
	"fmt"
	tp "net/textproto"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// all times are in local time
type IrcConnection struct {
	conn       *tp.Conn
	initTime   time.Time
	updateTime time.Time
	nick       string
	user       string
	real       string
	channel    string
}

func NewIrcConnection(socket string, nick string, user string, real string) (*IrcConnection, error) {
	if newconn, err := tp.Dial("tcp", socket); err != nil {
		return nil, err
	} else {
		ic := IrcConnection{newconn, time.Now(), time.Now(), "", "", "", ""}
		ic.setNick(nick)
		ic.setUserReal(user, real)
		return &ic, err
	}
}

// container of all our IRC metadata
type Irc struct {
	conns   []*IrcConnection
	current string // current socket
}

func (ic *IrcConnection) sendMessage(msg string) {
	fmt.Println("Sending message:", msg)
	fmt.Fprint(ic.conn.Writer.W, msg+"\r\n")
	ic.conn.Writer.W.Flush()
	ic.updateTime = time.Now()
}

func (ic *IrcConnection) setNick(newNick string) {
	ic.sendMessage("NICK " + newNick)
	ic.nick = newNick
}

func (ic *IrcConnection) setUserReal(user string, real string) {
	ic.sendMessage("USER " + user + " 0 * :" + real)
	ic.user = user
	ic.real = real
}

// this should probably be in a thread since messages can happen any time
func (ic *IrcConnection) listen() {
	for line, err := ic.conn.R.ReadString('\n'); err == nil; {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PING") {
			fmt.Println("ping!")
			ic.pong(line)
		} else {
			fmt.Println(line)
		}
	}
}

func (ic *IrcConnection) pong(ping string) {
	ic.sendMessage(strings.Replace(ping, "PING", "PONG", 1))
}

func (i *Irc) addConnection(socket string, nick string, user string, real string) (*IrcConnection, bool) {
	// add the connection to the conns map
	if conn, err := NewIrcConnection(socket, nick, user, real); err != nil {
		fmt.Println("Failed to add connection to %q! Error is: %q", socket, err)
		return conn, false
	} else {
		i.conns = append(i.conns, conn)
		// start the listen thread
		go conn.listen()
		return conn, true
	}
}

func (ic *IrcConnection) quit(message string) {
	ic.sendMessage("QUIT :" + message)
}

// With the given prompt, get input, returning only when it's more than
// white space
func readWithPrompt(prompt string, r *bufio.Reader) string {
	val := ""
	for len(val) == 0 {
		fmt.Print(prompt)
		val, _ = r.ReadString('\n')
		val = strings.TrimSpace(val)
	}
	return val
}

func (i *Irc) handleTermination() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// close every connection
	go func() {
		sig := <-sigs
		fmt.Println("Received " + sig.String() + ", quitting all active chats...")
		for _, ic := range i.conns {
			ic.quit("Program terminated")
		}
		os.Exit(0)
	}()
}

func main() {
	var (
		i      Irc
		reader = bufio.NewReader(os.Stdin)
		ready  = false
		ic     *IrcConnection
	)
	// prepare for program termination
	i.handleTermination()
	for !ready {
		ic, ready = i.addConnection(
			readWithPrompt("Server name: ", reader),
			readWithPrompt("Nickname: ", reader),
			readWithPrompt("Username: ", reader),
			readWithPrompt("Real Name: ", reader))
	}
	// Send user input directly to IRC server
	for {
		ic.sendMessage(readWithPrompt("Type 'JOIN #<channel>' to join a channel:", reader))
	}
}
