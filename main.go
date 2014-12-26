package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type user struct {
	userID        string
	locationIndex int
	position      position
	viewPortX     int
	viewPortY     int
	modal         map[string]rune
	killModal     chan bool
	energy        int // limit actions to energy
	life          int
	deaths        int
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
	result      chan commandStatus
}

type commandStatus struct {
	err        error
	message    string
	statusCode int
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

		cmdResult := make(chan commandStatus)
		listener <- command{cmd: cmd, userID: userID, result: cmdResult}

		result := <-cmdResult

		w.WriteHeader(result.statusCode)
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
		wrld.users[userID] = user{
			userID:    userID,
			position:  startingPosition,
			viewPortX: width,
			viewPortY: height,
			modal:     loadModal(help()),
			life:      5,
		}
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

		if cmd.cmd[0] == '>' {
			statusCode := http.StatusOK
			log.Println("command processing")
			thisCmd := strings.ToLower(strings.TrimSpace(cmd.cmd[1:]))
			cmdPart := strings.Split(thisCmd, " ")
			switch cmdPart[0] {
			case "help":
				tmpUser := wrld.users[cmd.userID]
				tmpUser.killModal <- true
				tmpUser.modal = loadModal(help())
				wrld.users[cmd.userID] = tmpUser

			case "clear":
				tmpUser := wrld.users[cmd.userID]
				tmpUser.killModal <- true
				tmpUser.modal = loadModal("")
				wrld.users[cmd.userID] = tmpUser

			case "resize":
				if len(cmdPart) == 3 {
					tmpUser := wrld.users[cmd.userID]
					// todo - err handling
					width, _ := strconv.Atoi(cmdPart[1])
					height, _ := strconv.Atoi(cmdPart[2])
					tmpUser.viewPortX = width
					tmpUser.viewPortY = height
					wrld.users[cmd.userID] = tmpUser
				}
			case "profile":
				go func(w *world, userID string) {
					// do ...
					tmpUser := w.users[userID]
					tmpUser.modal = loadModal(tmpUser.profileModal())
					wrld.users[userID] = tmpUser
					// while ...
					for {
						select {
						case <-time.Tick(time.Millisecond * 1000):
							// may need a mutex
							tmpUser := w.users[userID]
							tmpUser.modal = loadModal(tmpUser.profileModal())
							wrld.users[userID] = tmpUser
						case <-w.users[userID].killModal:
							return
						}
					}
				}(wrld, cmd.userID)
			case "attack":
				// get all units in range and deal damage
				// if their life falls to >0, recreate them
				x, y := wrld.users[cmd.userID].position.x, wrld.users[cmd.userID].position.y
				log.Printf("user %s at (%d,%d) attack", cmd.userID, x, y)
				for i := x - 1; i <= x+1; i++ {
					for j := y - 1; j <= y+1; j++ {
						if !(i == x && j == y) {
							// don't damage current user
							curPos := fmt.Sprintf("%d,%d", i, j)
							if pos, ok := wrld.locations[0].positions[curPos]; ok {
								if pos.userID != "" {
									tmp_user := wrld.users[pos.userID]
									tmp_user.life--
									wrld.users[pos.userID] = tmp_user
									if wrld.users[pos.userID].life <= 0 {
										// plase damaged user at start
										tmp_user := wrld.users[pos.userID]
										tmp_user.position.x = 2
										tmp_user.position.y = 3
										tmp_user.deaths++
										tmp_user.life = 5
										wrld.users[pos.userID] = tmp_user
										// clear out the previous cell
										tmp_pos := wrld.locations[0].positions[curPos]
										tmp_pos.character = ' '
										tmp_pos.closed = false
										tmp_pos.userID = ""
										wrld.locations[0].positions[curPos] = tmp_pos
									}
								}
							}
						}
					}
				}
			default:
				statusCode = http.StatusNotImplemented
			}
			// a console command demands a response
			cmd.result <- commandStatus{statusCode: statusCode}
			continue
		}
		// all other commands just need to not block
		cmd.result <- commandStatus{statusCode: http.StatusOK}

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

	// offsetY := 0
	// offsetX := 0

	visibilityX := 15
	visibilityY := 10

	for y := 1; y <= height; y++ {
		for x := 1; x <= width; x++ {
			// WAT
			// do something better here for the translation
			translationX := -1*(wrld.users[uid].viewPortX/2) + userX + x
			translationY := -1*(wrld.users[uid].viewPortY/2) + userY + y
			cell := fmt.Sprintf("%d,%d", translationX, translationY)
			pos := wrld.locations[0].positions[cell]
			theRune := ' '

			if r, ok := wrld.users[uid].modal[fmt.Sprintf("%d,%d", x, y)]; ok {
				theRune = r
			} else if pos == nil {
				theRune = '·'
			} else if abs(translationX-userX) > visibilityX || abs(translationY-userY) > visibilityY {
				theRune = '·'
			} else if pos.userID != "" {
				// todo: depending on user class, use different symbols and colors
				theRune = '◊'
			} else {
				theRune = pos.character
			}

			body = append(body, theRune)

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

func loadModal(s string) map[string]rune {
	m := make(map[string]rune)
	x, y := 0, 0
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		y++
		x = 0
		for _, r := range line {
			x++
			m[fmt.Sprintf("%d,%d", x, y)] = r
		}
	}
	// log.Println(x, y, m)
	return m
}

func help() string {
	return `
┌──────────────────────────────┐
│ Help                         │▒
│ Basic info                   │▒
╞══════════════════════════════╡▒
│ Each command must be started │▒
│ with ":".                    │▒
│                              │▒
│ - help   - clear    - resize │▒
│ - attack - . (redo) - profile│▒
│                              │▒
└──────────────────────────────┘▒
 ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒
`
}

func (u *user) profileModal() string {
	return fmt.Sprintf(`
┌─────────────────────────────┐
│ User Info      %12s │▒
╞═════════════════════════════╡▒
│ Life:   %3d                 │▒
│ Energy: %3d                 │▒
│ Deaths: %3d                 │▒
│                             │▒
└─────────────────────────────┘▒
 ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒
`, u.userID, u.life, u.energy, u.deaths)
}
