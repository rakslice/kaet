package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"text/template"
	"bytes"
	"net/url"
)

var (
	cmdPrefixes []string
	quotes      *store
	cmds        *commands
	evtMsgs     *store
)

type commands struct {
	sync.RWMutex
	cmds     map[string]*command
	aliases  map[string]string
	rAliases map[string][]string
	store    *store
}

type command struct {
	fn      func(string) string
	modOnly bool
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

	evtMsgs = Store("eventmessages")

	quotes = Store("quotes")

	cmds = &commands{
		cmds:     map[string]*command{},
		aliases:  map[string]string{},
		rAliases: map[string][]string{},
		store:    Store("commands"),
	}

	// Dynamic commands
	for _, k := range cmds.store.Keys() {
		v, _ := cmds.store.Get(k)
		cmds.cmds[k] = &command{func(_ string) string { return v }, false}
	}

	// Pleb commands
	cmds.cmds["help"] = &command{cmdHelp, false}
	cmds.cmds["uptime"] = &command{func(_ string) string { return getUptime(CHANNEL) }, false}
	cmds.cmds["game"] = &command{func(_ string) string { return getGame(CHANNEL, true) }, false}
	cmds.cmds["quote"] = &command{func(q string) string { return quotes.Random(q) }, false}
	cmds.cmds["sourcecode"] = &command{func(q string) string { return "Contribute to kaet's source code at github.com/Fugiman/kaet VoHiYo" }, false}

	// Mod commands
	cmds.cmds["addquote"] = &command{cmdAddQuote, true}
	cmds.cmds["addcommand"] = &command{cmdAddCommand, true}
	cmds.cmds["removecommand"] = &command{cmdRemoveCommand, true}

	// Event message commands
	cmds.cmds["seteventmessage"] = &command{cmdSetEventMessage, true}
	cmds.cmds["removeeventmessage"] = &command{cmdRemoveEventMessage, true}
	cmds.cmds["testeventmessage"] = &command{cmdTestEventMessage, true}

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

// Fields accessible in the event message template
type EvtMessageParams struct {
	Username string
}

var allEvents = map[string]bool{
	"newsub": true,
}

func NumberOfSubs() (int, error) {
	subsCount := struct {
		Total *int `json:"_total"`
	}{}

	getParams := make(url.Values)
	getParams.Add("limit", "0")

	//FIXME hardcoded channel name
	err := kraken(&subsCount, "channels", "kate", "subscriptions?" + getParams.Encode())
	if err != nil {
		return 0, err
	}
	if subsCount.Total == nil {
		return 0, fmt.Errorf("subscriptions response was missing total")
	}
	return *subsCount.Total, nil
}

func prepTemplate(event string, msgTemplate string, params *EvtMessageParams) (*template.Template, error) {
	tmpl := template.New(event)
	tmpl.Funcs(map[string]interface{} {
		"NumberOfSubs": NumberOfSubs,
	})
	tmpl, err := tmpl.Parse(msgTemplate)
	if err != nil {
		return nil, err
	}
	return tmpl, err
}

func doEvt(event string, params EvtMessageParams) (string, error) {
	_, evtFound := allEvents[event]
	if !evtFound {
		fmt.Fprintf(os.Stderr, "WARNING: Event '%s' is missing from allEvents so users will not be able to set a message", event)
	}

	msgTemplate, found := evtMsgs.Get(event)
	if !found {
		return "", nil
	}

	// TODO keep the prepped template around
	tmpl, err := prepTemplate(event, msgTemplate, &params)
	if err != nil {
		return "", err
	}

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

//FIXME hardcoded channel name
const subMessageSuffix = " just subscribed to kate!"

func handle(out chan string, m *message) {
	switch m.Command {
	case "PING":
		out <- fmt.Sprintf("PONG :%s\r\n", strings.Join(m.Args, " "))
	case "RECONNECT":
		os.Exit(69)
	case "PRIVMSG":
		if strings.HasPrefix(m.Prefix, "twitchnotify!twitchnotify@") && strings.HasSuffix(m.Args[1], subMessageSuffix) {
			username := m.Args[1][:len(m.Args[1])-len(subMessageSuffix)]
			// TODO keep track of the number of subs in the bot in case the API doesn't update fast enough
			eventOutput, err := doEvt("newsub", EvtMessageParams{username})
			if err != nil {
				fmt.Fprintf(os.Stderr, "evt sub returned error: %s\n", err)
			}
			if eventOutput != "" {
				out <- eventOutput
			}
		}

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

func cmdAddCommand(data string) string {
	cmds.Lock()
	defer cmds.Unlock()
	v := split(data, 2)
	trigger, msg := strings.TrimPrefix(v[0], "!"), v[1]
	cmds.store.Add(trigger, msg)
	cmds.cmds[trigger] = &command{func(_ string) string { return msg }, false}
	return ""
}

func cmdRemoveCommand(data string) string {
	cmds.Lock()
	defer cmds.Unlock()
	v := split(data, 2)
	trigger := strings.TrimPrefix(v[0], "!")
	cmds.store.Remove(trigger)
	delete(cmds.cmds, trigger)
	return ""
}

func cmdSetEventMessage(data string) string {
	v := split(data, 2)
	event := v[0]
	msgTemplate := v[1]

	_, exists := allEvents[event]
	if !exists {
		eventNames := make([]string, 0, len(allEvents))
		for event := range allEvents {
			eventNames = append(eventNames, event)
		}

		return fmt.Sprintf("There's no event '%s'. Available events: %s", event, strings.Join(eventNames, ", "))
	}

	// test prepare the template to check for problems
	_, prepErr := prepTemplate(event, msgTemplate, &EvtMessageParams{})

	if prepErr != nil {
		return fmt.Sprintf("Template error: %s", prepErr)
	}

	evtMsgs.Add(event, msgTemplate)
	return fmt.Sprintf("Message for %s set.", event)
}

func cmdRemoveEventMessage(event string) string {
	_, exists := evtMsgs.Get(event)
	if exists {
		evtMsgs.Remove(event)
		return fmt.Sprintf("Message for %s removed.", event)
	} else {
		return fmt.Sprintf("There is already no message for %s", event)
	}
}

func cmdTestEventMessage(data string) string {
	v := split(data, 2)
	event := v[0]

	username := v[1]

	params := EvtMessageParams{username}

	eventOutput, err := doEvt(event, params)
	if err != nil {
		return fmt.Sprintf("ERROR: %s", err)
	}
	return eventOutput
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
