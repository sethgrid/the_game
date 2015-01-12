package main

import (
	"io/ioutil"
	"log"
	"testing"
)

func TestMapUser(t *testing.T) {
	log.SetOutput(ioutil.Discard)
	w := genWorld("maps/map_1.map", 0, 1)

	xPos, yPos := 2, 3
	w.createUser("testingUser", 80, 20, position{x: xPos, y: yPos}, false)

	if got := w.users["testingUser"].position.x; got != xPos {
		t.Errorf("unexpected x position. got %d, want %d", got, xPos)
	}

	if got := w.users["testingUser"].position.y; got != yPos {
		t.Errorf("unexpected y position. got %d, want %d", got, yPos)
	}

	listener := make(chan command)
	cmdResult := make(chan commandStatus)

	go gameRunner(w, listener)

	// move left (a)
	listener <- command{cmd: "ma", userID: "testingUser", result: cmdResult}
	<-cmdResult
	if got := w.users["testingUser"].position.x; got != xPos {
		t.Errorf("unexpected x position. user should not have moved (blocked by wall). got %d, want %d", got, xPos)
	}

	// move right (d)
	listener <- command{cmd: "md", userID: "testingUser", result: cmdResult}
	<-cmdResult
	if got := w.users["testingUser"].position.x; got != xPos+1 {
		t.Errorf("unexpected x position. user should have moved. got %d, want %d", got, xPos+1)
	}

}

func TestMapCapacity(t *testing.T) {
	log.SetOutput(ioutil.Discard)
	w := genWorld("maps/map_1.map", 0, 1)

	created := w.createUser("testingUser", 80, 20, position{x: 2, y: 3}, false)
	if !created {
		t.Error("Unable to create user")
	}

	created = w.createUser("testingUser2", 80, 20, position{x: 2, y: 3}, false)
	if created {
		t.Error("Should not create more users than capacity allows")
	}

	created = w.createUser("testingUser", 80, 20, position{x: 2, y: 3}, false)
	if !created {
		t.Error("Unable to re-create existing user after capacity is met")
	}
}
