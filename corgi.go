package main

import (
	"bufio"
	"fmt"
	"github.com/natemealey/corgi/utils"
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
}

func NewIrcConnection(socket string, nick string, user string, real string) (*IrcConnection, error) {
	if newconn, err := tp.Dial("tcp", socket); err != nil {
		return nil, err
	} else {
		ic := IrcConnection{newconn, time.Now(), time.Now(), "", "", ""}
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

func NewIrc() *Irc {
	var i Irc
	// prepare for program termination
	i.handleTermination()
	i.current = ""
	return &i
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

// this should be run in a goroutine since messages can happen any time
func (ic *IrcConnection) listen() {
	line := ""
	for err := error(nil); err == nil; line, err = ic.conn.R.ReadString('\n') {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PING") {
			ic.sendMessage(strings.Replace(line, "PING", "PONG", 1))
		} else {
			fmt.Println(line)
		}
	}
}

func (i *Irc) addConnection(socket string, nick string, user string, real string) (*IrcConnection, bool) {
	// add the connection to the conns map
	if ic, err := NewIrcConnection(socket, nick, user, real); err != nil {
		fmt.Println("Failed to add connection to %q! Error is: %q", socket, err)
		return ic, false
	} else {
		i.conns = append(i.conns, ic)
		// start the listen thread
		go ic.listen()
		return ic, true
	}
}

func (ic *IrcConnection) quit(message string) {
	ic.sendMessage("QUIT :" + message)
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

// if run as the only program, supply a simple command line interface
func main() {
	var (
		i      = NewIrc()
		reader = bufio.NewReader(os.Stdin)
		ready  = false
		ic     *IrcConnection
	)
	for !ready {
		ic, ready = i.addConnection(
			utils.ReadWithPrompt("Server name: ", reader),
			utils.ReadWithPrompt("Nickname: ", reader),
			utils.ReadWithPrompt("Username: ", reader),
			utils.ReadWithPrompt("Real Name: ", reader))
	}
	// Send user input directly to IRC server
	for {
		ic.sendMessage(utils.ReadWithPrompt("", reader))
	}
}
