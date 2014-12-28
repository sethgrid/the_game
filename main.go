package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecheney/profile"

	_ "net/http/pprof"
)

type user struct {
	userID      string
	ID          int
	viewPortX   int
	viewPortY   int
	position    position
	killChan    chan bool
	commChan    chan string // not yet in use
	modal       map[string]rune
	activeModal string
	lastCommand time.Time

	isNPC     bool
	energy    int
	life      int
	deaths    int
	kills     int
	character rune
}

func (p position) String() string {
	return fmt.Sprintf("%d,%d", p.x, p.y)
}

type command struct {
	cmd, userID, monsterID string
	result                 chan commandStatus
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

type position struct {
	x, y        int
	closed      bool
	description string
	character   rune
	userID      string
}

func main() {
	log.Println("Starting")
	defer profile.Start(profile.CPUProfile).Stop()

	listener := make(chan command)

	w := genWorld()

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

func genWorld() *world {
	loc := make([]location, 1)
	loc[0] = location{
		description: "init location",
		display:     []byte("some map"),
		positions:   loadMap("maps/map_1.map"),
	}
	commands := make([]command, 0)
	w := &world{locations: loc,
		capacity: 10, // not currently honored
		commands: commands,
		users:    make(map[string]user),
	}

	// spawn monsters
	saturationRate := 4
	opens := make([]*position, 0)
	openCount := 0
	// todo - don't count user spawn area as open
	for _, pos := range w.locations[0].positions {
		if pos.closed == false {
			openCount++
			opens = append(opens, pos)
		}
	}
	rand.Seed(time.Now().Unix())
	for monsterCount := saturationRate * openCount / 100; monsterCount >= 0; monsterCount-- {
		idx := rand.Intn(len(opens))
		pos := opens[idx]

		randInt := rand.Intn(2000000000)

		w.createUser(strconv.Itoa(randInt), 80, 20, *pos, true)
	}
	return w
}

func getWorld(wrld *world) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// todo sanitize
		width, _ := strconv.Atoi(r.FormValue("w"))
		height, _ := strconv.Atoi(r.FormValue("h"))
		wrld.createUser(r.FormValue("uid"), width, height, position{x: 2, y: 3}, false)
		w.Write(wrld.display(r.FormValue("uid"), width, height))
	}
}

func receiveCommand(listener chan command) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd := strings.TrimSpace(r.FormValue("key"))
		userID := strings.TrimSpace(r.FormValue("uid"))
		monsterID := strings.TrimSpace(r.FormValue("mid"))

		// validate
		// todo - userID vs userName (nonce vs human readable)
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

		// HEAP? could the compiler hold onto cmdResult references?
		cmdResult := make(chan commandStatus)
		listener <- command{cmd: cmd, userID: userID, monsterID: monsterID, result: cmdResult}

		result := <-cmdResult

		w.WriteHeader(result.statusCode)
		w.Write([]byte(result.message))
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
		c := time.Tick(time.Millisecond * 100)
		for _ = range c {
			wrld.updateBoard()
		}
	}()
}

func (wrld *world) createUser(userID string, width, height int, startingPosition position, isNPC bool) {
	if _, found := wrld.users[userID]; found {
		return
	}
	log.Printf("New user '%s' (%d,%d)", userID, startingPosition.x, startingPosition.y)

	maxLife := 3
	maxEnergey := 150
	rand.Seed(int64(time.Now().Nanosecond()))
	characters := []rune{'◊', 'ᐉ', 'ᛤ', '៙', '⁖', '⁘', '⁙', '⊙', '⍾', '⎔', '⎊', '⎈', '◈', '☆', '☃', '☢', '☣', '♀', '♂', '⚉', '♜', '⛄'}
	randChar := characters[rand.Intn(len(characters))]

	comm := make(chan string)
	kill := make(chan bool)

	wrld.users[userID] = user{
		position:    startingPosition,
		viewPortX:   width,
		viewPortY:   height,
		commChan:    comm,
		killChan:    kill,
		energy:      maxEnergey / 10,
		life:        maxLife,
		character:   randChar,
		isNPC:       isNPC,
		lastCommand: time.Now(),
		modal:       loadModal(help()),
		userID:      userID,
	}

	// todo - instead of passing in the world, pass in a channel tied to this user
	// the the user can have its own for select goro that takes in mutations to the user

	go func(w *world, userID string) {
		dur := time.Minute * 10
		c := time.Tick(dur)
		for _ = range c {
			if time.Now().Unix() > w.users[userID].lastCommand.Add(dur).Unix() {
				log.Println("Inactive", userID)
				close(w.users[userID].killChan)
				//time.Sleep(time.Second * 1)

				pos := w.users[userID].position.String()
				delete(w.users, userID)

				tmpPos := w.locations[0].positions[pos]
				tmpPos.character = ' '
				tmpPos.closed = false
				tmpPos.userID = ""
				w.locations[0].positions[pos] = tmpPos
				return
			}
		}
	}(wrld, userID)

	go func(w *world, userID string) {
		c := time.Tick(time.Second * 5)
		for _ = range c {
			tmpUser := w.users[userID]
			select {
			case _, ok := <-w.users[userID].killChan:
				if !ok {
					return
				}
			default:
				if tmpUser.life < maxLife {
					tmpUser.life++
				}
				w.users[userID] = tmpUser
			}
		}
	}(wrld, userID)

	go func(w *world, userID string) {
		c := time.Tick(time.Millisecond * 500)
		for _ = range c {
			tmpUser := w.users[userID]
			select {
			case _, ok := <-w.users[userID].killChan:
				if !ok {
					return
				}
			default:
				if tmpUser.energy < maxEnergey {
					tmpUser.energy++
				}
				w.users[userID] = tmpUser
			}
		}
	}(wrld, userID)

	if isNPC {
		// todo - have a goro that handles this monster until it dies
		go func(w *world, mID string) {
			rDur := (time.Duration)(rand.Intn(1000) + 400)
			c := time.Tick(time.Millisecond * rDur)
			for _ = range c {

				// check if user is close by and attack
				// but make monsters unable to attack monsters

				x, y := wrld.users[mID].position.x, wrld.users[mID].position.y
				for i := x - 1; i <= x+1; i++ {
					for j := y - 1; j <= y+1; j++ {
						if !(i == x && j == y) {
							wrld.Lock()
							cell := fmt.Sprintf("%d,%d", i, j)
							opponentID := wrld.locations[0].positions[cell].userID
							isNPC := wrld.users[opponentID].isNPC
							wrld.Unlock()
							if opponentID != "" && !isNPC {
								_, err := http.Get(fmt.Sprintf("http://localhost:8888/cmd?uid=%s&key=>attack", mID))
								if err != nil {
									log.Println(err)
								}
								continue
							}
						}
					}
				}

				// move in random direction or (todo) towards user
				direction := ""
				switch rand.Intn(4) {
				case 0:
					direction = "mw"
				case 1:
					direction = "ma"
				case 2:
					direction = "ms"
				case 3:
					direction = "md"
				}
				_, err := http.Get(fmt.Sprintf("http://localhost:8888/cmd?uid=%s&key=%s", mID, direction))
				if err != nil {
					log.Println(err)
				}

				if w.users[mID].deaths > 0 {
					tmpPos := w.locations[0].positions[w.users[mID].position.String()]
					tmpPos.character = ' '
					tmpPos.userID = ""
					tmpPos.closed = false
					w.locations[0].positions[w.users[mID].position.String()] = tmpPos
					delete(w.users, mID)
					return
				}
			}
		}(wrld, userID)
	}
}

func (wrld *world) updateBoard() {
	// play through commands and clear the list
	wrld.Lock()
	defer wrld.Unlock()

	if len(wrld.commands) == 0 {
		return
	}

	for _, cmd := range wrld.commands {
		{
			tmpUser := wrld.users[cmd.userID]
			tmpUser.lastCommand = time.Now()
			wrld.users[cmd.userID] = tmpUser
		}

		if cmd.cmd[0] == '>' {
			log.Println(cmd)
			statusCode := http.StatusOK
			message := ""
			thisCmd := strings.ToLower(strings.TrimSpace(cmd.cmd[1:]))
			cmdPart := strings.Split(thisCmd, " ")
			switch cmdPart[0] {
			case "help":
				tmpUser := wrld.users[cmd.userID]
				tmpUser.activeModal = "help"
				tmpUser.modal = loadModal(help())
				wrld.users[cmd.userID] = tmpUser

			case "clear":
				tmpUser := wrld.users[cmd.userID]
				tmpUser.activeModal = ""
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
					tmpUser.activeModal = "profile"
					wrld.users[userID] = tmpUser
					// while ...
					c := time.Tick(time.Millisecond * 500)
					for _ = range c {
						tmpUser := w.users[userID]
						if tmpUser.activeModal != "profile" {
							return
						}
						tmpUser.modal = loadModal(tmpUser.profileModal())
						wrld.users[userID] = tmpUser
					}
				}(wrld, cmd.userID)
			case "attack":
				// get all units in range and deal damage
				// if their life falls to >0, recreate them
				attackEnergy := 15
				if wrld.users[cmd.userID].energy < attackEnergy {
					message = "Not enough energy"
					cmd.result <- commandStatus{statusCode: statusCode, message: message}

					continue
				}

				tmpUser := wrld.users[cmd.userID]
				tmpUser.energy -= attackEnergy
				wrld.users[cmd.userID] = tmpUser

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
										// todo: if isNPC - place is non existant location?
										{
											tmpUser := wrld.users[pos.userID]
											tmpUser.position.x = 2
											tmpUser.position.y = 3
											tmpUser.deaths++
											tmpUser.life = 5
											wrld.users[pos.userID] = tmpUser
										}
										{
											tmpUser := wrld.users[cmd.userID]
											tmpUser.kills++
											wrld.users[cmd.userID] = tmpUser
										}
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
			cmd.result <- commandStatus{statusCode: statusCode, message: message}
			continue
		}
		// all other commands just need to not block
		// could move this above each continue to give feedback to clients
		cmd.result <- commandStatus{statusCode: http.StatusOK}

		curPos := wrld.users[cmd.userID].position
		newPos := applyMove(curPos, cmd.cmd)

		if _, ok := wrld.locations[0].positions[newPos.String()]; !ok {
			if curPos.String() == "0,0" {
				log.Println("attempting to move non-existant user?")
			}
			continue
		}
		if wrld.locations[0].positions[newPos.String()].closed {
			continue
		}

		// https://github.com/golang/go/issues/3117
		// cannot yet assign to a field of a map indirectly
		tmpUser := wrld.users[cmd.userID]
		if tmpUser.energy <= 0 {
			continue
		}
		tmpUser.position = newPos
		tmpUser.energy--
		wrld.users[cmd.userID] = tmpUser

		// update former/current position first. new pos may overwrite it,
		// and we want to keep the most current information
		tmpPosA := wrld.locations[0].positions[curPos.String()]
		tmpPosA.closed = false
		tmpPosA.userID = ""
		wrld.locations[0].positions[curPos.String()] = tmpPosA

		tmpPosB := wrld.locations[0].positions[newPos.String()]
		tmpPosB.closed = true
		tmpPosB.userID = cmd.userID
		wrld.locations[0].positions[newPos.String()] = tmpPosB

	}

	// clear the played through commands
	wrld.commands = make([]command, 0)
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
				theRune = wrld.users[pos.userID].character
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
┌──────────────────────────────────┐
│ Help                             │▒
│ Basic info                       │▒
╞══════════════════════════════════╡▒
│ Movement: w,a,s,d                │▒
│ Attack: x                        │▒
│                                  │▒
│ Console commands must be started │▒
│ with ":".                        │▒
│                                  │▒
│ - help   - clear    - resize     │▒
│ - attack - . (redo) - profile    │▒
│                                  │▒
└──────────────────────────────────┘▒
 ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒
`
}

func (u *user) profileModal() string {
	return fmt.Sprintf(`
┌─────────────────────────────┐
│ User Info    %12s %3c │▒
╞═════════════════════════════╡▒
│ Life:   %3d     Deaths: %3d │▒
│ Energy: %3d     Kills:  %3d │▒
│                             │▒
└─────────────────────────────┘▒
 ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒
`, u.userID, u.character, u.life, u.deaths, u.energy, u.kills)
}
