package main

import (
	"bufio"
	"fmt"
	gp "github.com/natemealey/GoPanes"
	"github.com/natemealey/corgi/utils"
	tp "net/textproto"
	"os"
	"os/signal"
	"sort"
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
		panes.Root.Second.MakeEditable()
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

func (ui *IrcUi) Close() {
	ui.panes.Close()
}

func (ui *IrcUi) render() {
	ui.panes.Root.Refresh()
}

func (ui *IrcUi) GetLine() string {
	return ui.inputBox.GetLine()
}

func (ui *IrcUi) Alive() bool {
	return ui.inputBox.IsAlive()
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

// semantic sytax coloring
func (ui *IrcUi) info(line string) {
	ui.output(utils.Color.Blue(line))
}

func (ui *IrcUi) err(line string) {
	ui.output(utils.Color.Red(line))
}

func (ui *IrcUi) warn(line string) {
	ui.output(utils.Color.Yellow(line))
}

func (ui *IrcUi) success(line string) {
	ui.output(utils.Color.Green(line))
}

func (ui *IrcUi) note(line string) {
	ui.output(utils.Color.DarkGray(line))
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
			nick:       nick,
			channels:   make(map[string]*Channel)}
		ic.setNick(nick)
		ic.setUserReal(user, real)
		return &ic, err
	}
}

type Channel struct {
	name       string
	nicks      map[string]bool // acts as a set
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

func (ic *IrcServer) sendMessage(msg string) {
	fmt.Fprint(ic.conn.Writer.W, msg+"\r\n")
	ic.conn.Writer.W.Flush()
	ic.updateTime = time.Now()
}

func (ic *IrcServer) setNick(newNick string) {
	ic.nick = newNick
	// TODO the nick isn't always set - see error cases at
	// https://tools.ietf.org/html/rfc1459#section-4.1.2
	ic.sendMessage("NICK " + newNick)
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
	if sender == "" {
		sender = ic.nick
	}
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

// translated from the Twisted implementation
func parseIrcMessage(message string) (prefix, command string, args []string) {
	prefix = ""
	var trailing string
	if len(message) == 0 {
		// TODO better error handling
		return prefix, command, args
	}
	if message[0] == ':' {
		strs := strings.SplitN(message[1:], " ", 2)
		if len(strs) > 0 {
			prefix = strs[0]
		}
		if len(strs) > 1 {
			message = strs[1]
		}
	}
	if strings.Contains(message, " :") {
		strs := strings.SplitN(message, " :", 2)
		if len(strs) > 0 {
			message = strs[0]
		}
		if len(strs) > 1 {
			trailing = strs[1]
		}
		args = strings.Split(message, " ")
		args = append(args, trailing)
	} else {
		args = strings.Split(message, " ")
	}
	command, args = args[0], args[1:]
	return prefix, command, args
}

func prefixToSender(prefix string) string {
	return strings.Replace(strings.Split(prefix, "!")[0], ":", "", 1)
}

func (ic *IrcServer) logLineToChannel(line, channelName string) {
	if ic.channels[channelName] != nil {
		ic.channels[channelName].logs = append(ic.channels[channelName].logs, line)
	}
}

// TODO this is a disgustingly long function
// TODO remove necessity of ui param and return a string to be printed
// by whatever to improve decoupling
// returns the name of the channel that send the line
func (ic *IrcServer) handleLine(line string, ui *IrcUi) string {
	// first, append the line to the logs
	var (
		channelName string
		message     string
	)
	// TODO handle taken nick, MOTD end
	prefix, command, args := parseIrcMessage(line)
	if len(args) > 0 {
		channelName = args[0]
	}
	if prefix != "" || command != "" {
		if len(args) > 1 {
			message = args[len(args)-1]
		}
		sender := prefixToSender(prefix)
		switch command {
		case "JOIN":
			ic.joinChannel(channelName, sender)
			if ic.nick == sender {
				ui.clearOutput()
			}
			if ic.currentChannel != nil && ic.currentChannel.name == channelName {
				ui.note(sender + " has joined " + channelName)
			}
			ic.channels[channelName].nicks[sender] = true
		case "PART":
			if ic.currentChannel.name == channelName {
				ui.note(sender + " has parted " + channelName)
			}
			ic.leaveChannel(channelName, sender)
		case "PRIVMSG":
			// TODO handle new private messages
			ic.printMessage(sender, channelName, message, ui)
		case "QUIT":
			if ic.currentChannel != nil && ic.currentChannel.nicks[sender] {
				ui.note(sender + " has quit.")
				ic.currentChannel.nicks[sender] = false
			}
		case "NICK":
			newNick := channelName
			if ic.nick == sender {
				ui.note("Your nickname is now " + newNick)
				ic.nick = newNick
				// TODO re-render prompt
			}
			for _, channel := range ic.channels {
				if channel.nicks[sender] {
					if channel == ic.currentChannel {
						ui.note(sender + " is now known as " + newNick)
					}
					channel.nicks[sender] = false
					channel.nicks[newNick] = true
				}
			}
		case "KICK":
			victim := channelName
			ic.leaveChannel(channelName, victim)
			if victim == ic.nick {
				ui.note("You have been kicked from " + channelName + " by " + sender)
			} else {
				if ic.currentChannel.name == channelName {
					ui.note(victim + " was kicked from " + channelName + " by " + sender)
				}
			}
		case "353": // list of nicks
			nicks := strings.Fields(message)
			// get actual channel name
			if len(args) > 2 {
				channelName = args[2]
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
		case "366": // End of nicks
		case "375": // MOTD start
		case "372": // MOTD body
		case "376": // MOTD end
		default:
			ui.note(strings.Join(args[1:], " "))
		}
	}
	return channelName
}

// this should be run in a goroutine since messages can happen any time
// TODO is passing the UI pointer sensible?
func (ic *IrcServer) listen(ui *IrcUi) {
	var channelName string
	var line string
	for err := error(nil); err == nil; line, err = ic.conn.R.ReadString('\n') {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PING") {
			ic.sendMessage(strings.Replace(line, "PING", "PONG", 1))
		} else {
			channelName = ic.handleLine(line, ui)
			ic.logLineToChannel(line, channelName)
		}
	}
}

func (ic *IrcServer) quit(message string) {
	ic.sendMessage("QUIT :" + message)
}

// container of all our IRC server metadata
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

// Must be called on program exit to clean up after UI
func (sm *ServerManager) Close() {
	sm.ui.Close()
}

// Adds a connection to the manager and sets it as the current server
func (sm *ServerManager) addConnection(socket string, nick string, user string, real string) (*IrcServer, bool) {
	// add the connection to the conns map
	if ic, err := NewIrcServer(socket, nick, user, real); err != nil {
		sm.ui.err("Failed to add connection to " + socket + "! Error is: " + err.Error())
		return ic, false
	} else {
		sm.ui.success("Successfully connected to " + socket)
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
		sm.ui.info("Received " + sig.String() + ", quitting all active chats...")
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
	case "server":
		sm.newServer(args)
	case "servers":
		sm.outputServers(args)
	case "nick":
		sm.setNick(args)
	case "nicks":
		sm.outputNicks(args)
	case "usr":
		sm.setUser(args)
	case "help":
		sm.outputHelp(args)
	default:
		sm.ui.err(cmd + " is an unrecognized command!")
	}
}

func (sm *ServerManager) messageCurrent(args string) {
	if sm.current == nil {
		sm.ui.err("Not on any server!")
		return
	}
	if sm.current.currentChannel == nil {
		sm.ui.err("No current channel selected!")
		return
	}
	sm.message(sm.current.currentChannel.name + " " + args)
}
func (sm *ServerManager) message(args string) {
	strs := strings.SplitN(args, " ", 2)
	if len(strs) < 2 {
		sm.ui.err("Must specify a channel and message text!")
		return
	}
	target := strs[0]
	message := strs[1]
	line := "PRIVMSG " + target + " :" + message
	sm.current.sendMessage(line)
	sm.current.printMessage(sm.current.nick, target, message, sm.ui)
	sm.current.logLineToChannel(line, target)
}
func (sm *ServerManager) away(args string) {}
func (sm *ServerManager) quitAll(args string) {
	for _, ic := range sm.servers {
		ic.quit("Quit command received")
	}
	sm.Close()
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
			sm.ui.success("Switched to " + newName)
			return
		}
	}
	sm.ui.err("No such channel " + newName + "!")
}

// join just one channel
func (sm *ServerManager) joinChannel(args string) {
	// extract channel name
	channelName := strings.Fields(args)[0]
	sm.ui.note("Joining " + channelName + "...")
	sm.current.sendMessage("JOIN " + channelName)
	// channel is added and set as current when server sends JOIN back
}

func (sm *ServerManager) newServer(args string) {
	strs := strings.Fields(args)
	if len(strs) > 1 {
		// TODO is there a better default port location?
		// TODO handle
		sm.addConnection(strs[0]+":"+strs[1], "", "corgi.def", "corgi.def")
	} else if len(strs) > 0 {
		sm.addConnection(strs[0]+":6667", "", "corgi.def", "corgi.def")
	}

}
func (sm *ServerManager) outputServers(args string) {}

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
		sm.ui.err("Cannot part: no active channel and no channel specified")
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
		sm.ui.err("Must specify a nick!")
		return
	} else if strings.Contains(newNick, " ") {
		sm.ui.err("Nick cannot contain spaces!")
		return
	}
	if sm.current != nil {
		sm.current.setNick(newNick)
	} else {
		sm.ui.warn("Can't set nick, must connect to a server")
	}
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
		sm.ui.err("Can't output nicks: no active channel and no channel specified")
		return
	}
	if sm.current.channels[channelName] == nil {
		sm.ui.err("Couldn't look up channel '" + channelName + "'!")
		return
	}
	nicks := make([]string, len(sm.current.channels[channelName].nicks))
	idx := 0
	for nick := range sm.current.channels[channelName].nicks {
		nicks[idx] = nick
		idx++
	}
	sort.Strings(nicks)

	sm.ui.output(utils.Color.Blue("All nicks on "+channelName+": ") + utils.Color.Magenta(strings.Join(nicks, " ")))
}

func (sm *ServerManager) outputHelp(args string) {}

func (sm *ServerManager) handleUserInput(input string) {
	// split into command + args
	if strings.HasPrefix(input, "/") {
		cmd := strings.Fields(input)[0]
		sm.processCommand(cmd[1:], strings.TrimSpace(strings.Replace(input, cmd, "", 1)))
	} else {
		sm.processCommand("", input)
	}
}

func main() {
	var (
		sm    = NewServerManager()
		input string
	)
	defer sm.Close()
	sm.ui.success("Initialized Corgi IRC client")
	// read in args, if any
	args := os.Args[1:]
	if len(args) > 0 {
		sm.ui.note("Handling commands: `" + strings.Join(args, "`, `") + "`")
	}
	for _, arg := range args {
		sm.handleUserInput(arg)
	}
	for sm.ui.Alive() {
		// read in line from user
		/*if sm.current != nil {
			input = sm.ui.getMessageWithPrompt(
				utils.Color.Magenta(sm.current.nick) + utils.Color.Blue("> "))
		} else {
			input = sm.ui.getMessageWithPrompt(
				utils.Color.Magenta("[not on any server]") + utils.Color.Blue("> "))
		}*/
		input = sm.ui.GetLine()
		sm.handleUserInput(input)
	}
}
