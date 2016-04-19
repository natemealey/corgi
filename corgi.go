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
type IrcServer struct {
	conn           *tp.Conn
	initTime       time.Time
	updateTime     time.Time
	socket         string
	nick           string
	user           string
	real           string
	channels       map[string]*Channel
	currentChannel *Channel
}

func NewIrcServer(socket string, nick string, user string, real string) (*IrcServer, error) {
	// TODO default port of 6667
	if newconn, err := tp.Dial("tcp", socket); err != nil {
		return nil, err
	} else {
		ic := IrcServer{
			conn:       newconn,
			socket:     socket,
			initTime:   time.Now(),
			updateTime: time.Now(),
			channels:   make(map[string]*Channel)}
		ic.setNick(nick)
		ic.setUserReal(user, real)
		return &ic, err
	}
}

// TODO does this need other data?
type Channel struct {
	name       string
	nicks      map[string]bool //basically acts as a set
	logs       []string
	initTime   time.Time
	updateTime time.Time
}

func NewChannel(channelName string) *Channel {
	return &Channel{
		name:       channelName,
		initTime:   time.Now(),
		updateTime: time.Now(),
		nicks:      make(map[string]bool)}
}

// container of all our IRC server metadata
// TODO does this even make sense as a type?
type ServerManager struct {
	servers []*IrcServer
	current *IrcServer // current socket
}

func NewServerManager() *ServerManager {
	var sm ServerManager
	// prepare for program termination
	sm.handleTermination()
	return &sm
}

func (ic *IrcServer) sendMessage(msg string) {
	fmt.Println("Sending message:", msg)
	fmt.Fprint(ic.conn.Writer.W, msg+"\r\n")
	ic.conn.Writer.W.Flush()
	ic.updateTime = time.Now()
}

func (ic *IrcServer) setNick(newNick string) {
	ic.sendMessage("NICK " + newNick)
	ic.nick = newNick
}

func (ic *IrcServer) setUserReal(user string, real string) {
	ic.sendMessage("USER " + user + " 0 * :" + real)
	ic.user = user
	ic.real = real
}

// when a user parts or quits a channel, select the next most recently added
// assumes that the current channel was already removed
func (ic *IrcServer) selectNextChannel() {
	// only try to find another channel if there will be one available after the
	// current channel is removed
	var nextChannel *Channel
	for name, channel := range ic.channels {
		if name != ic.currentChannel.name && (nextChannel == nil || channel.updateTime.After(nextChannel.updateTime)) {
			nextChannel = channel
		}
	}
	ic.currentChannel = nextChannel
	ic.currentChannel.updateTime = time.Now()
}

// returns true if the destName is of the current channel or nick
// TODO is this sensible?
func (ic *IrcServer) isCurrent(destName string) bool {
	return (ic.currentChannel != nil && ic.currentChannel.name == destName) || ic.nick == destName
}

// TODO this is a disgustingly long function
func (ic *IrcServer) handleLine(line string) {
	// first, append the line to the logs
	var (
		channelName string
		message     string
	)
	// TODO handle nicks
	// TODO handle taken nick, MOTD end
	strs := strings.SplitN(line, " ", 4)
	sender := strings.Replace(strings.Split(strs[0], "!")[0], ":", "", 1)
	if len(strs) > 2 {
		channelName = strs[2]
	}
	if len(strs) > 3 {
		message = strings.Replace(strs[3], ":", "", 1)
	}
	if len(strs) > 1 {
		if strs[1] == "JOIN" {
			if ic.nick == sender {
				newChannel := NewChannel(channelName)
				ic.channels[channelName] = newChannel
				ic.currentChannel = newChannel
				ic.currentChannel.updateTime = time.Now()
			}
			if ic.currentChannel.name == channelName {
				fmt.Println(sender, "has joined", channelName)
			}
			ic.channels[channelName].nicks[sender] = true
		} else if strs[1] == "PART" {
			if ic.currentChannel.name == channelName {
				fmt.Println(sender, "has parted", channelName)
			}
			ic.channels[channelName].nicks[sender] = false
			if ic.nick == sender {
				// remove the current channel from the channels map
				delete(ic.channels, channelName)
				// if it's the current channel, move to the next one
				if ic.currentChannel.name == channelName {
					ic.selectNextChannel()
				}
			}
		} else if strs[1] == "PRIVMSG" {
			// TODO clean this up
			if ic.isCurrent(channelName) {
				fmt.Println(channelName, "<"+sender+">", message)
			}
		} else if strs[1] == "QUIT" {
		} else if strs[1] == "KICK" {
		} else if strs[1] == "353" { // 353 means a list of nicks
		}
	}
	// TODO should logs have a different format?
	if ic.currentChannel != nil && ic.currentChannel.name == channelName {
		ic.currentChannel.logs = append(ic.currentChannel.logs, line)
	}
}

// this should be run in a goroutine since messages can happen any time
func (ic *IrcServer) listen() {
	line := ""
	for err := error(nil); err == nil; line, err = ic.conn.R.ReadString('\n') {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PING") {
			ic.sendMessage(strings.Replace(line, "PING", "PONG", 1))
		} else {
			ic.handleLine(line)
		}
	}
}

func (ic *IrcServer) quit(message string) {
	ic.sendMessage("QUIT :" + message)
}

// Adds a connection to the manager and sets it as the current server
func (sm *ServerManager) addConnection(socket string, nick string, user string, real string) (*IrcServer, bool) {
	// add the connection to the conns map
	if ic, err := NewIrcServer(socket, nick, user, real); err != nil {
		fmt.Println("Failed to add connection to", socket, "! Error is: ", err)
		return ic, false
	} else {
		fmt.Println("Successfully connected to ", socket)
		sm.servers = append(sm.servers, ic)
		sm.current = ic
		// start the listen thread
		go ic.listen()
		return ic, true
	}
}

func (sm *ServerManager) handleTermination() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// close every connection
	go func() {
		sig := <-sigs
		fmt.Println("Received " + sig.String() + ", quitting all active chats...")
		sm.quitAll("")
	}()
}

// if run as the only program, supply a simple command line interface
// TODO this should probably be a more flexible init system
func InitWithServer() *ServerManager {
	var (
		sm     = NewServerManager()
		reader = bufio.NewReader(os.Stdin)
		ready  = false
	)
	for !ready {
		_, ready = sm.addConnection(
			utils.ReadWithPrompt("Server name: ", reader),
			utils.ReadWithPrompt("Nickname: ", reader),
			utils.ReadWithPrompt("Username: ", reader),
			utils.ReadWithPrompt("Real Name: ", reader))
	}
	// Send user input directly to IRC server
	return sm
}
func (sm *ServerManager) processCommand(cmd string, args string) {
	switch cmd {
	case "":
		sm.messageCurrent(args)
	case "msg":
		sm.message(args)
	case "away":
		sm.away(args)
	case "current":
		sm.outputCurrent(args)
	case "quit":
		sm.quitAll(args)
	case "join":
		sm.joinChannel(args)
	case "channel":
		sm.switchChannel(args)
	case "channels":
		sm.outputChannels(args)
	case "usr":
		sm.setUser(args)
	case "help":
		sm.outputHelp(args)
	default:
		// TODO be nicer to the user
		fmt.Println(cmd + " is an unrecognized command!")
	}
}

func (sm *ServerManager) messageCurrent(args string) {
	if sm.current.currentChannel == nil {
		fmt.Println("No current channel selected!")
		return
	}
	sm.current.sendMessage("PRIVMSG " + sm.current.currentChannel.name + " " + args)
}
func (sm *ServerManager) message(args string) {
	// get user or channel name
	target := " "
	message := strings.Replace(args, target+" ", "", 1)
	// format message
	sm.current.sendMessage("PRIVMSG " + target + " " + message)
}
func (sm *ServerManager) away(args string)          {}
func (sm *ServerManager) outputCurrent(args string) {}
func (sm *ServerManager) quitAll(args string) {
	for _, ic := range sm.servers {
		ic.quit("Quit command received")
	}
	os.Exit(0)
}
func (sm *ServerManager) switchChannel(args string) {
	newName := strings.TrimSpace(args)
	for _, channel := range sm.current.channels {
		if channel.name == newName {
			sm.current.currentChannel = channel
			channel.updateTime = time.Now()
			return
		}
	}
	fmt.Println("No such channel ", newName, "!")
}

// join just one channel
func (sm *ServerManager) joinChannel(args string) {
	// extract channel name
	channelName := strings.Fields(args)[0]
	sm.current.sendMessage("JOIN " + channelName)
	// channel is added and set as current when server sends JOIN back
}

// TODO is this even necessary?
func (sm *ServerManager) setUser(args string) {}
func (sm *ServerManager) outputChannels(args string) {
	fmt.Println("All connected channels on: ", sm.current.socket)
	// TODO this output isn't ordered - should we order by something?
	for _, channel := range sm.current.channels {
		if sm.current.currentChannel == channel {
			fmt.Println("  " + channel.name + " [active]")
		} else {
			fmt.Println("  " + channel.name)
		}
	}
}
func (sm *ServerManager) outputHelp(args string) {}

func main() {
	var (
		sm     = InitWithServer()
		input  string
		cmd    string
		reader = bufio.NewReader(os.Stdin)
	)
	fmt.Println("Initialized Corgi IRC client")
	// read in a user ident?
	for {
		// read in line from user
		input = utils.ReadWithPrompt(">", reader)
		// split into command + args
		if strings.HasPrefix(input, "/") {
			cmd = strings.Fields(input)[0][1:]
		} else {
			cmd = ""
		}
		// process command + args
		// TODO this could almost certainly be cleaner
		sm.processCommand(cmd, strings.Replace(input, "/"+cmd+" ", "", 1))
	}
}
