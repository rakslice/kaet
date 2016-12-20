package main

import (
	"testing"
	"fmt"
	"runtime/debug"
	"strings"
)

func assertEqualStr(t *testing.T, expected string, actual string) bool {
	if actual != expected {
		fmt.Printf("Error: Output was '%s'; expected '%s'\n", actual, expected)
		debug.PrintStack()
		t.Fail()
		return true
	}
	return false
}

func assertEqualInt(t *testing.T, expected int, actual int) bool {
	if actual != expected {
		fmt.Printf("Error: Output was '%v'; expected '%v'\n", actual, expected)
		debug.PrintStack()
		t.Fail()
		return true
	}
	return false
}

func TestPrivMsg(t *testing.T) {
	line := []byte(":SomeNick!~someuser@unaffiliated/something PRIVMSG iambot :!help\r\n")

	var m *message = parse(line)
	m.Sub = true
	m.Mod = false

	if assertEqualStr(t, "SomeNick!~someuser@unaffiliated/something", m.Prefix) {
		return
	}

	out := make(chan string, 1000)
	handle(out, m)
	if assertEqualInt(t, 1, len(out)) {
		return
	}

	sentMessage := <-out
	if !strings.HasPrefix(sentMessage, "PRIVMSG iambot :​Available Commands: ") {
		t.Fail(); return
	}
	if !strings.HasSuffix(sentMessage, "\r\n") {
		t.Fail(); return
	}
}
