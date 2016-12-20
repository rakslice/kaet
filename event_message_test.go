package main

import (
	"testing"
	"github.com/jarcoal/httpmock"
)

func TestOtherChannelSub(t *testing.T) {
	line := []byte(":twitchnotify!twitchnotify@twitchnotify.tmi.twitch.tv PRIVMSG #kate :someusername just subscribed to kate!\r\n")
	m := parse(line)

	evtMsgs.Add("newsub", "Welcome to the channel, {{.Username}}")

	out := make(chan string, 5000)
	handle(out, m)
	if assertEqualInt(t, 1, len(out)) {
		return
	}

	assertEqualStr(t, "Welcome to the channel, someusername", <-out)
}

func TestSubMessageWithCount(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	line := []byte(":twitchnotify!twitchnotify@twitchnotify.tmi.twitch.tv PRIVMSG #kate :someusername just subscribed to kate!\r\n")
	m := parse(line)

	evtMsgs.Add("newsub", "Welcome slice #{{NumberOfSubs}}, {{.Username}}!")

	cannedResponse := `{
  "_total": 42,
  "_links": {
    "next": "https://api.twitch.tv/kraken/channels/test_channel/subscriptions?limit=25&offset=25",
    "self": "https://api.twitch.tv/kraken/channels/test_channel/subscriptions?limit=25&offset=0"
  },
  "subscriptions": []
}`

	httpmock.RegisterResponder("GET", "https://api.twitch.tv/kraken/channels/kate/subscriptions",
		httpmock.NewStringResponder(200, cannedResponse))

	out := make(chan string, 5000)
	handle(out, m)
	if assertEqualInt(t, 1, len(out)) {
		return
	}

	assertEqualStr(t, "Welcome slice #42, someusername!", <-out)
}