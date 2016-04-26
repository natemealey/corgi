package main

import (
	"bufio"
	"fmt"
	gp "github.com/natemealey/GoPanes"
	"github.com/natemealey/corgi/utils"
	tp "net/textproto"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type IrcUi struct {
	panes     *gp.GoPaneUi
	inputBox  *gp.GoPane
	outputBox *gp.GoPane
}

func NewIrcUi() *IrcUi {
	panes := gp.NewGoPaneUi()
	if panes.Root.Horiz(-4) {
		newUi := IrcUi{
			panes:     panes,
			inputBox:  panes.Root.Second,
			outputBox: panes.Root.First}

		newUi.render()
		return &newUi
	}
	fmt.Println("Failed to split")
	return nil
}

func (ui *IrcUi) render() {
	ui.panes.Clear()
	ui.panes.Root.Refresh()
}

func (ui *IrcUi) getMessageWithPrompt(prompt string) string {
	ui.inputBox.Clear()
	ui.panes.Root.Refresh()
	ui.inputBox.Focus()
	str := utils.ReadWithPrompt(prompt, bufio.NewReader(os.Stdin))
	return str
}

func (ui *IrcUi) clearOutput() {
	ui.outputBox.Clear()
	ui.outputBox.Refresh()
}

func (ui *IrcUi) output(line string) {
	ui.outputBox.AddLine(line)
	ui.outputBox.Refresh()
}

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
	ui      *IrcUi
}

func NewServerManager() *ServerManager {
	var sm ServerManager
	// prepare for program termination
	sm.handleTermination()
	sm.ui = NewIrcUi()
	return &sm
}

func (ic *IrcServer) sendMessage(msg string) {
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
	if ic.currentChannel != nil {
		ic.currentChannel.updateTime = time.Now()
	}
}

func (ic *IrcServer) printMessage(sender string, recipient string, msg string, ui *IrcUi) {
	// if it's a private message, output accordingly
	if !strings.HasPrefix(recipient, "#") {
		ui.output(utils.Color.Yellow(recipient) + " " +
			utils.Color.Magenta("<"+sender+">") + " " +
			utils.Color.Blue("[private]") + " " +
			msg)
	} else if ic.currentChannel != nil && ic.currentChannel.name == recipient {
		ui.output(utils.Color.Yellow(recipient) + " " +
			utils.Color.Magenta("<"+sender+">") + " " +
			msg)
	}
	// TODO make sure this works
	if ic.channels[recipient] != nil {
		line := ":" + sender + " PRIVMSG " + recipient + " :" + msg
		ic.channels[recipient].logs = append(ic.channels[recipient].logs, line)
	}
}

func (ic *IrcServer) joinChannel(channelName, nick string) {
	if ic.nick == nick {
		newChannel := NewChannel(channelName)
		ic.channels[channelName] = newChannel
		ic.currentChannel = newChannel
		ic.currentChannel.updateTime = time.Now()
	}
}

func (ic *IrcServer) leaveChannel(channelName, nick string) {
	ic.channels[channelName].nicks[nick] = false
	if ic.nick == nick {
		// remove the current channel from the channels map
		delete(ic.channels, channelName)
		// if it's the current channel, move to the next one
		if ic.currentChannel.name == channelName {
			ic.selectNextChannel()
		}
	}
}

// TODO this is a disgustingly long function
// TODO remove necessity of ui param and return a string to be printed
// by whatever to improve decoupling
func (ic *IrcServer) handleLine(line string, ui *IrcUi) {
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
	// TODO evaluate the quality of this
	if len(strs) > 3 {
		split := strings.SplitN(line, ":", 3)
		if len(split) < 3 {
			message = strings.Replace(strs[3], ":", "", 1)
		} else {
			message = split[2]
		}
	}
	if len(strs) > 1 {
		if strs[1] == "JOIN" {
			ic.joinChannel(channelName, sender)
			if ic.nick == sender {
				ui.clearOutput()
			}
			if ic.currentChannel.name == channelName {
				ui.output(utils.Color.DarkGray(sender + " has joined " + channelName))
			}
			ic.channels[channelName].nicks[sender] = true
		} else if strs[1] == "PART" {
			if ic.currentChannel.name == channelName {
				ui.output(utils.Color.DarkGray(sender + " has parted " + channelName))
			}
			ic.leaveChannel(channelName, sender)
		} else if strs[1] == "PRIVMSG" {
			ic.printMessage(sender, channelName, message, ui)
		} else if strs[1] == "QUIT" {
			if ic.currentChannel.name == channelName {
				ui.output(utils.Color.DarkGray(sender + " has quit " + channelName))
			}
		} else if strs[1] == "KICK" {
		} else if strs[1] == "353" { // list of nicks
			nicks := strings.Fields(message)
			// get actual channel name
			// TODO make sure params will be split this way always
			params := strings.SplitN(line, " ", 6)
			if len(params) == 6 {
				channelName = strings.TrimSpace(params[4])
			}
			for _, nick := range nicks {
				// discard operator prefixes
				if strings.HasPrefix(nick, "+") || strings.HasPrefix(nick, "@") {
					nick = nick[1:]
				}
				// add the nick to the channel's nick list
				if ic.channels[channelName] != nil {
					ic.channels[channelName].nicks[nick] = true
				}
			}
		} else if strs[1] == "366" { // End of nicks
		} else if strs[1] == "375" { // MOTD start
		} else if strs[1] == "372" { // MOTD body
		} else if strs[1] == "376" { // MOTD end
		} else {
			ui.output(utils.Color.DarkGray(message))
		}
	}
	if ic.channels[channelName] != nil {
		ic.channels[channelName].logs = append(ic.channels[channelName].logs, line)
	}
}

// this should be run in a goroutine since messages can happen any time
// TODO is passing the UI pointer sensible?
func (ic *IrcServer) listen(ui *IrcUi) {
	line := ""
	for err := error(nil); err == nil; line, err = ic.conn.R.ReadString('\n') {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PING") {
			ic.sendMessage(strings.Replace(line, "PING", "PONG", 1))
		} else {
			ic.handleLine(line, ui)
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
		sm.ui.output("Failed to add connection to " + socket + "! Error is: " + err.Error())
		return ic, false
	} else {
		sm.ui.output("Successfully connected to " + socket)
		sm.servers = append(sm.servers, ic)
		sm.current = ic
		// start the listen thread
		go ic.listen(sm.ui)
		return ic, true
	}
}

func (sm *ServerManager) handleTermination() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// close every connection
	go func() {
		sig := <-sigs
		sm.ui.output("Received " + sig.String() + ", quitting all active chats...")
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
		sm.ui.inputBox.Focus()
		_, ready = sm.addConnection(
			utils.ReadWithPrompt(utils.Color.Blue("Server name: "), reader),
			utils.ReadWithPrompt(utils.Color.Blue("Nickname: "), reader),
			utils.ReadWithPrompt(utils.Color.Blue("Username: "), reader),
			utils.ReadWithPrompt(utils.Color.Blue("Real Name: "), reader))
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
	case "quit":
		sm.quitAll(args)
	case "join":
		sm.joinChannel(args)
	case "part":
		sm.partChannel(args)
	case "channel":
		sm.switchChannel(args)
	case "channels":
		sm.outputChannels(args)
	case "nicks":
		sm.outputNicks(args)
	case "usr":
		sm.setUser(args)
	case "nick":
		sm.setNick(args)
	case "help":
		sm.outputHelp(args)
	default:
		// TODO be nicer to the user
		sm.ui.output(cmd + " is an unrecognized command!")
	}
}

func (sm *ServerManager) messageCurrent(args string) {
	if sm.current.currentChannel == nil {
		sm.ui.output("No current channel selected!")
		return
	}
	sm.current.sendMessage("PRIVMSG " + sm.current.currentChannel.name + " :" + args)
	sm.current.printMessage(sm.current.nick, sm.current.currentChannel.name, args, sm.ui)
}
func (sm *ServerManager) message(args string) {
	strs := strings.SplitN(args, " ", 2)
	if len(strs) < 2 {
		sm.ui.output("Must specify a channel and message text!")
		return
	}
	target := strs[0]
	message := strs[1]
	sm.current.sendMessage("PRIVMSG " + target + " :" + message)
	sm.current.printMessage(sm.current.nick, target, message, sm.ui)
}
func (sm *ServerManager) away(args string) {}
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
			sm.ui.clearOutput()
			for _, line := range channel.logs {
				sm.current.handleLine(line, sm.ui)
			}
			sm.ui.output(utils.Color.DarkGray("Switched to " + newName))
			return
		}
	}
	sm.ui.output("No such channel " + newName + "!")
}

// join just one channel
func (sm *ServerManager) joinChannel(args string) {
	// extract channel name
	channelName := strings.Fields(args)[0]
	sm.ui.output(utils.Color.DarkGray("Joining " + channelName + "..."))
	sm.current.sendMessage("JOIN " + channelName)
	// channel is added and set as current when server sends JOIN back
}

// only works when the first arg is the channel
// extracts channel from arg list, substituting current channel if none was specified.
// error condition if no current channel and no channel specified.
// returns true, "" on error, false, channelName on success
func (sm *ServerManager) channelFromArgs(args string) (bool, string) {
	channelName := ""
	// extract channel name
	if args == "" {
		if sm.current.currentChannel == nil {
			return true, ""
		}
		channelName = sm.current.currentChannel.name
	} else {
		channelName = strings.TrimSpace(strings.Fields(args)[0])
	}
	return false, channelName
}

func (sm *ServerManager) partChannel(args string) {
	err, channelName := sm.channelFromArgs(args)
	if err {
		sm.ui.output("Cannot part: no active channel and no channel specified")
		return
	}
	sm.current.sendMessage("PART " + channelName)
	// channel is added and set as current when server sends JOIN back
}

// TODO is this even necessary?
func (sm *ServerManager) setUser(args string) {}
func (sm *ServerManager) setNick(args string) {
	newNick := strings.TrimSpace(args)
	if len(newNick) == 0 {
		sm.ui.output("Must specify a nick!")
		return
	} else if strings.Contains(newNick, " ") {
		sm.ui.output("Nick cannot contain spaces!")
		return
	}
	sm.current.setNick(newNick)
}
func (sm *ServerManager) outputChannels(args string) {
	sm.ui.output(utils.Color.Blue("All connected channels on: ") + utils.Color.Magenta(sm.current.socket))
	// TODO this output isn't ordered - should we order by something?
	for _, channel := range sm.current.channels {
		if sm.current.currentChannel == channel {
			sm.ui.output("  " + utils.Color.Yellow(channel.name) + utils.Color.Green(" [active]"))
		} else {
			sm.ui.output("  " + utils.Color.Yellow(channel.name))
		}
	}
}
func (sm *ServerManager) outputNicks(args string) {
	err, channelName := sm.channelFromArgs(args)
	if err {
		sm.ui.output("Can't output nicks: no active channel and no channel specified")
		return
	}
	if sm.current.channels[channelName] == nil {
		sm.ui.output("Couldn't look up channel '" + channelName + "'!")
		return
	}
	nickList := ""
	for nick, _ := range sm.current.channels[channelName].nicks {
		nickList += nick + " "
	}
	sm.ui.output(utils.Color.Blue("All nicks on "+channelName+": ") + utils.Color.Magenta(nickList))
}

func (sm *ServerManager) outputHelp(args string) {}

func main() {
	var (
		sm    = InitWithServer()
		input string
		cmd   string
	)
	sm.ui.output("Initialized Corgi IRC client")
	for {
		// read in line from user
		input = sm.ui.getMessageWithPrompt(
			utils.Color.Magenta(sm.current.nick) + utils.Color.Blue("> "))
		// split into command + args
		if strings.HasPrefix(input, "/") {
			cmd = strings.Fields(input)[0]
			sm.processCommand(cmd[1:], strings.TrimSpace(strings.Replace(input, cmd, "", 1)))
		} else {
			sm.processCommand("", input)
		}
	}
}
