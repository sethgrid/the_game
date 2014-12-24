package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type user struct {
	userID        string
	locationIndex int
	position      position
	viewPortX     int
	viewPortY     int
}

type position struct {
	x, y        int
	closed      bool
	description string
	character   rune
	userID      string
}

func (p position) String() string {
	return fmt.Sprintf("%d,%d", p.x, p.y)
}

type command struct {
	cmd, userID string
}

type world struct {
	locations []location
	capacity  int

	sync.Mutex
	commands []command
	users    map[string]user
}

type location struct {
	description string
	display     []byte
	positions   map[string]*position

	sync.Mutex
}

func main() {
	log.Println("Starting")

	listener := make(chan command)

	loc := make([]location, 1)
	loc[0] = location{
		description: "init location",
		display:     []byte("some map"),
		positions:   loadMap("maps/map_1.map"),
	}
	commands := make([]command, 0)
	w := &world{locations: loc,
		capacity: 10,
		commands: commands,
		users:    make(map[string]user),
	}

	go gameRunner(w, listener)

	http.HandleFunc("/", getWorld(w))
	http.HandleFunc("/cmd", receiveCommand(listener))

	log.Println("Registered /")
	log.Println("Registered /cmd?uid=[string]&key=[char]")

	log.Println("Listening on :8888")

	err := http.ListenAndServe(":8888", nil)
	if err != nil {
		log.Fatal(err)
	}
}

func getWorld(wrld *world) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// todo sanitize
		width, _ := strconv.Atoi(r.FormValue("w"))
		height, _ := strconv.Atoi(r.FormValue("h"))
		wrld.createUser(r.FormValue("uid"), width, height)
		w.Write(wrld.display(r.FormValue("uid"), width, height))
	}
}

func receiveCommand(listener chan command) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd := r.FormValue("key")
		userID := r.FormValue("uid")
		// validate
		if userID == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("provide a uid string"))
			return
		}
		if cmd == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("provide a key pressed"))
			return
		}

		listener <- command{cmd: cmd, userID: userID}
	}
}

func gameRunner(wrld *world, listener chan command) {
	go func() {
		for {
			select {
			case cmd := <-listener:
				wrld.Lock()
				wrld.commands = append(wrld.commands, cmd)
				wrld.Unlock()
			}
		}
	}()

	go func() {
		for {
			select {
			case <-time.Tick(time.Millisecond * 100):
				wrld.updateBoard()
			}
		}
	}()
}

func (wrld *world) createUser(userID string, width, height int) {
	startingPosition := position{x: 2, y: 3}

	if _, found := wrld.users[userID]; !found {
		log.Println("New user", userID)
		wrld.users[userID] = user{userID: userID, position: startingPosition, viewPortX: width, viewPortY: height}
	}
}

func (wrld *world) updateBoard() {
	// play through commands and clear the list
	wrld.Lock()
	defer wrld.Unlock()

	if len(wrld.commands) == 0 {
		return
	}

	log.Println("Commands:")
	for _, cmd := range wrld.commands {
		log.Println(cmd)

		curPos := wrld.users[cmd.userID].position
		newPos := applyMove(curPos, cmd.cmd)

		if _, ok := wrld.locations[0].positions[newPos.String()]; !ok {
			log.Printf("attempted location is non existant")
			continue
		}
		if wrld.locations[0].positions[newPos.String()].closed {
			log.Printf("attempted location is closed (%s) -> (%s)\n", curPos.String(), newPos.String())
			continue
		}

		// https://github.com/golang/go/issues/3117
		// cannot yet assign to a field of a map indirectly
		tmp_a := wrld.users[cmd.userID]
		tmp_a.position = newPos
		wrld.users[cmd.userID] = tmp_a

		// update former/current position first. new pos may overwrite it,
		// and we want to keep the most current information
		tmp_c := wrld.locations[0].positions[curPos.String()]
		tmp_c.closed = false
		tmp_c.userID = ""
		wrld.locations[0].positions[curPos.String()] = tmp_c

		tmp_b := wrld.locations[0].positions[newPos.String()]
		tmp_b.closed = true
		tmp_b.userID = cmd.userID
		wrld.locations[0].positions[newPos.String()] = tmp_b
	}

	// clear the played through commands
	wrld.commands = make([]command, 0)
	log.Println()
}

func (wrld *world) display(uid string, width, height int) []byte {
	body := make([]rune, 0)

	userX := wrld.users[uid].position.x
	userY := wrld.users[uid].position.y

	offsetY := (wrld.users[uid].viewPortY) / 2
	offsetX := (wrld.users[uid].viewPortY) / 2

	visibilityX := 12
	visibilityY := 8

	for y := 1 - offsetY; y <= height-offsetY; y++ {
		for x := 1 - offsetX; x <= width-offsetX; x++ {
			cell := fmt.Sprintf("%d,%d", x, y)
			pos := wrld.locations[0].positions[cell]
			if pos == nil {
				continue
			}

			if abs(userX-x) > visibilityX || abs(userY-y) > visibilityY {
				body = append(body, ' ')
			} else if pos.userID != "" {
				// todo: depending on user class, use different symbols and colors
				body = append(body, '◊')
			} else {
				body = append(body, pos.character)
			}

		}
		body = append(body, '\n')
	}

	return []byte(string(body))
}

func abs(i int) int {
	if i < 0 {
		return i * -1
	}
	return i
}

func applyMove(p position, s string) position {
	pNew := p
	switch s {
	case "mw":
		pNew.y--
	case "ma":
		pNew.x--
	case "ms":
		pNew.y++
	case "md":
		pNew.x++
	}
	log.Printf("old pos, %+v, new pos %+v", p, pNew)
	return pNew
}

func loadMap(path string) map[string]*position {
	var err error
	var Rune rune
	theMap := make(map[string]*position)

	fh, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	br := bufio.NewReader(fh)

	x, y := 0, 1
	for err == nil {
		Rune, _, err = br.ReadRune()
		x++
		if Rune == rune('\n') {
			y++
			x = 0
			continue
		}
		closed := false
		if Rune != rune(' ') {
			closed = true
		}
		theMap[fmt.Sprintf("%d,%d", x, y)] = &position{
			x:         x,
			y:         y,
			character: Rune,
			closed:    closed,
		}

	}

	return theMap
}
