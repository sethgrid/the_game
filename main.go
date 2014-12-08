package main

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/sethgrid/curse"
)

type coordinate struct {
	x, y int
}

type position struct {
	backgroundColor int
	color           int
}

type screen struct {
	width, height int
	surface       map[coordinate]*position
	cursor        *curse.Cursor
}

func main() {
	f, err := os.OpenFile("logfile", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(f)
	log.Println("starting")
	_, err = curse.New()
	if err != nil {
		log.Fatal("unable to initialize curse environment - ", err)
	}

	screenX, screenY, err := curse.GetScreenDimensions()
	if err != nil {
		log.Fatal("unable to get screen dimensions - ", err)
	}

	s := NewScreen(screenX, screenY)

	s.cursor.Move(1, 1)
	input := bufio.NewReader(os.Stdin)
	for {
		command, err := input.ReadByte()
		if err != nil {
			fmt.Println(err)
		}
		if string(command) == "w" {
			s.cursor.MoveUp(1)
			log.Println("w")
		} else if string(command) == "s" {
			s.cursor.MoveDown(1)
			log.Println("w")
		} else if string(command) == "a" {
			s.cursor.MoveLeft(1)
			log.Println("a")
		} else if string(command) == "d" {
			s.cursor.MoveRight(1)
			log.Println("d")
		} else if string(command) == "q" {
			log.Println("q", "quiting")
			s.cursor.Move(1, 1)
			s.cursor.SetDefaultStyle()
			s.cursor.EraseAll()
			break
		}
		log.Println("new pos: ", s.cursor.Position)

		s.Paint()
	}
}

func NewScreen(x, y int) *screen {
	newScreen := &screen{width: x, height: y}
	newScreen.surface = make(map[coordinate]*position)
	newScreen.cursor, _ = curse.New()
	newScreen.Init()
	return newScreen
}

func (s *screen) Init() {
	for y := 1; y < s.height; y++ {
		for x := 1; x < s.width; x++ {
			s.surface[coordinate{x: x, y: y}] = &position{color: curse.RED}
		}
	}
	s.cursor.Move(1, 1)
	log.Println("init pos:", s.cursor.Position)
	s.Paint()
}

func (s *screen) Paint() {
	currentX, currentY := s.cursor.Position.X, s.cursor.Position.Y
	s.cursor.Move(1, 1)
	s.cursor.EraseDown()
	s.cursor.SetBackgroundColor(curse.WHITE).SetColor(curse.BLACK)
	for y := 0; y < s.height; y++ {
		s.cursor.EraseCurrentLine()
		for x := 0; x < s.width; x++ {
			if s.cursor.Position.X == x && s.cursor.Position.Y == y {
				fmt.Printf("O")
			} else {
				fmt.Printf(".")
			}
		}
		s.cursor.MoveDown(1)
	}
	s.cursor.Move(currentX, currentY)
}
