package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/go-lua"
)

var (
	cmdPrefixes []string
	quotes      *store
	cmds        *commands
)

type commands struct {
	sync.RWMutex
	cmds     map[string]*command
	aliases  map[string]string
	rAliases map[string][]string
	store    *store
	scriptStore *store
	luaState *lua.State
}

type command struct {
	fn      func(string) string
	modOnly bool
	removable bool
}

func (c *commands) Alias(alias, actual string) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.cmds[actual]; !ok {
		panic(fmt.Errorf("Invalid alias: %s -> %s", alias, actual))
	}
	c.aliases[alias] = actual
	c.rAliases[actual] = append(c.rAliases[actual], alias)
}

func (c *commands) Get(key string) *command {
	c.RLock()
	defer c.RUnlock()
	if nKey, ok := c.aliases[key]; ok {
		key = nKey
	}
	return c.cmds[key]
}

func init() {
	cmdPrefixes = []string{"!", USER + " ", fmt.Sprintf("@%s ", USER)}

	quotes = Store("quotes")

	cmds = &commands{
		cmds:     map[string]*command{},
		aliases:  map[string]string{},
		rAliases: map[string][]string{},
		store:    Store("commands"),
		scriptStore:	Store("scripts"),
		luaState: lua.NewState(),
	}

	lua.BaseOpen(cmds.luaState)
	// don't load package functions
	// no coroutine functions available
	lua.StringOpen(cmds.luaState)
	lua.TableOpen(cmds.luaState)
	lua.MathOpen(cmds.luaState)
	lua.Bit32Open(cmds.luaState)
	// don't load io functions
	// don't load os functions
	lua.DebugOpen(cmds.luaState)

	// Dynamic commands
	for _, k := range cmds.store.Keys() {
		v, _ := cmds.store.Get(k)
		cmds.cmds[k] = &command{func(_ string) string { return v }, false, true}
	}

	for _, k := range cmds.scriptStore.Keys() {
		v, _ := cmds.scriptStore.Get(k)
		setScriptForTrigger(k, v)
	}

	// Pleb commands
	cmds.cmds["help"] = &command{cmdHelp, false, false}
	cmds.cmds["uptime"] = &command{func(_ string) string { return getUptime(CHANNEL) }, false, false}
	cmds.cmds["game"] = &command{func(_ string) string { return getGame(CHANNEL, true) }, false, false}
	cmds.cmds["quote"] = &command{cmdGetQuote, false, false}
	cmds.cmds["sourcecode"] = &command{func(q string) string { return "Contribute to kaet's source code at github.com/Fugiman/kaet VoHiYo" }, false, false}

	// Mod commands
	cmds.cmds["addquote"] = &command{cmdAddQuote, true, false}
	cmds.cmds["removequote"] = &command{cmdRemoveQuote, true, false}
	cmds.cmds["addcommand"] = &command{cmdAddCommand, true, false}
	cmds.cmds["removecommand"] = &command{cmdRemoveCommand, true, false}
	cmds.cmds["addscript"] = &command{cmdAddScript, true, false}

	// Aliases
	cmds.Alias("halp", "help")
	cmds.Alias("add", "addcommand")
	cmds.Alias("addcom", "addcommand")
	cmds.Alias("remove", "removecommand")
	cmds.Alias("removecom", "removecommand")
	cmds.Alias("del", "removecommand")
	cmds.Alias("delcom", "removecommand")
	cmds.Alias("delcommand", "removecommand")
	cmds.Alias("source", "sourcecode")
	cmds.Alias("code", "sourcecode")
}

func handle(out chan string, m *message) {
	switch m.Command {
	case "PING":
		out <- fmt.Sprintf("PONG :%s\r\n", strings.Join(m.Args, " "))
	case "RECONNECT":
		os.Exit(69)
	case "PRIVMSG":
		msg := strings.ToLower(m.Args[1])
		for _, prefix := range cmdPrefixes {
			if strings.HasPrefix(msg, prefix) {
				p := split(m.Args[1][len(prefix):], 2)
				if c := cmds.Get(p[0]); c != nil && (!c.modOnly || m.Mod) {
					if response := c.fn(p[1]); response != "" {
						out <- fmt.Sprintf("PRIVMSG %s :\u200B%s\r\n", m.Args[0], response)
					}
				}
				return
			}
		}
	}
}

func cmdHelp(_ string) string {
	cmds.RLock()
	defer cmds.RUnlock()
	names := []string{}
	for k, _ := range cmds.cmds {
		names = append(names, k)
	}
	sort.Strings(names)
	return "Available Commands: " + strings.Join(names, " ")
}

func cmdAddQuote(quote string) string {
	g := getGame(CHANNEL, false)
	t := time.Now().Round(time.Second)
	if l, err := time.LoadLocation("America/Vancouver"); err == nil {
		t = t.In(l)
	}
	quotes.Append(fmt.Sprintf("%s [Playing %s - %s]", quote, g, t.Format(time.RFC822)))
	return ""
}

func cmdRemoveQuote(quoteNum string) string {
	if strings.HasPrefix(quoteNum, "#") {
		quoteNum = quoteNum[1:]
	}
	// cmdAddQuote relies on continuous numbering, so blank the quotes instead of removing them
	found := quotes.Blank(quoteNum)
	if found {
		return fmt.Sprintf("Removed #%s", quoteNum)
	} else {
		return ""
	}
}

func cmdGetQuote(query string) string {
	if strings.HasPrefix(query, "#") {
		quote, found := quotes.Get(query[1:])
		if found && quote != "" {
			return quote
		} else {
			return "Not found"
		}
	} else {
		return quotes.Random(query)
	}
}

func cmdAddCommand(data string) string {
	cmds.Lock()
	defer cmds.Unlock()
	v := split(data, 2)
	trigger, msg := strings.TrimPrefix(v[0], "!"), v[1]
	existingCmd, existingCmdFound := cmds.cmds[trigger]
	if (existingCmdFound && !existingCmd.removable) {
		return "I'm afraid I can't modify that command"
	}
	cmds.store.Add(trigger, msg)
	cmds.cmds[trigger] = &command{func(_ string) string { return msg }, false, true}
	return ""
}

func setScriptForTrigger(trigger string, script string) string {
	err := lua.LoadString(cmds.luaState, script)
	if err != nil {
		return fmt.Sprintf("script error: %s", err)
	}

	// returns any chat output indicating a problem with the script
	// is there a two step load / run option available so we can have less overhead each command run?
	cmds.cmds[trigger] = &command{func(input string) string {
		output := ""
		// TODO set up additional RegistryFunction instances for variables and functions we want to be available to the script
		setChatOutput := func(state *lua.State) int {
			n := state.Top()
			if n == 1 {
				val, ok := state.ToString(1)
				if ok {
					output = val
				}
			}
			return 0
		}
		cmds.luaState.PushGoFunction(setChatOutput)
		cmds.luaState.SetGlobal("setChatOutput")

		err := lua.DoString(cmds.luaState, script)

		// clear our output command
		cmds.luaState.PushNil()
		cmds.luaState.SetGlobal("setChatOutput")

		if err != nil {
			return fmt.Sprintf("script error: %s", err)
		} else {
			// script was successful
			return output
		}
	}, false, true}
	return ""
}

func cmdAddScript(data string) string {
	cmds.Lock()
	defer cmds.Unlock()
	v := split(data, 2)
	trigger, script := strings.TrimPrefix(v[0], "!"), v[1]
	existingCmd, existingCmdFound := cmds.cmds[trigger]
	if (existingCmdFound && !existingCmd.removable) {
		return "I'm afraid I can't modify that command"
	}
	cmds.scriptStore.Add(trigger, script)
	return setScriptForTrigger(trigger, script)
}

func cmdRemoveCommand(data string) string {
	cmds.Lock()
	defer cmds.Unlock()
	v := split(data, 2)
	trigger := strings.TrimPrefix(v[0], "!")
	existingCommand, existingCommandFound := cmds.cmds[trigger];
	if (existingCommandFound && !existingCommand.removable) {
		return "I'm afraid I can't remove that command"
	}
	cmds.store.Remove(trigger)
	delete(cmds.cmds, trigger)
	return ""
}

func split(s string, p int) []string {
	r := strings.SplitN(s, " ", p)
	for len(r) < p {
		r = append(r, "")
	}
	for i := 0; i < p-1; i++ {
		r[i] = strings.ToLower(r[i])
	}
	return r
}
