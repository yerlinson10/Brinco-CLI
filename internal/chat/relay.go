package chat

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"brinco-cli/internal/clipboard"
	"brinco-cli/internal/logx"
	"brinco-cli/internal/roomproto"
)

type relayHub struct {
	publicAddr string

	mu    sync.Mutex
	rooms map[string]*relayRoom

	maxPerIP int
	maxTotal int

	ipMu       sync.Mutex
	ipCounts   map[string]int
	totalConns int

	connMu sync.Mutex
	active map[net.Conn]struct{}
	wg     sync.WaitGroup
}

type relayRoom struct {
	id           string
	passwordHash string
	code         string

	mu      sync.Mutex
	clients map[*serverClient]struct{}
	muted   map[string]bool
	banned  map[string]bool
}

func RunCreateUsingRelay(name, relayAddr, password string) int {
	return RunCreateUsingRelayWithProtocol(name, relayAddr, password, roomproto.ProtocolRelay)
}

func RunCreateUsingRelayWithProtocol(name, relayAddr, password, protocol string) int {
	if strings.TrimSpace(relayAddr) == "" {
		fmt.Fprintln(os.Stderr, "Error: --relay es obligatorio en modo relay")
		return 1
	}
	if strings.TrimSpace(name) == "" {
		name = "host"
	}
	if !roomproto.IsKnown(protocol) {
		protocol = roomproto.ProtocolRelay
	}
	relays := SplitRelayList(relayAddr)
	best, results := SelectBestTCPRelay(relays, relayProbeTimeout)
	printRelayProbeResults(results)
	if best == "" {
		best = relayAddr
	}
	logx.Info("relay create start", "relay", best, "protocol", protocol)
	fmt.Printf("Conectando al relay %s...\n", best)
	return runRelayClient(best, "", name, password, "", true, protocol)
}

func RunRelayServer(listenAddr, publicAddr string, maxPerIP, maxTotal int) int {
	logx.Info("relay server start", "listen", listenAddr, "public", publicAddr, "maxPerIP", maxPerIP, "maxTotal", maxTotal)
	if strings.TrimSpace(listenAddr) == "" {
		listenAddr = "0.0.0.0:10000"
	}
	if strings.TrimSpace(publicAddr) == "" {
		if !isWildcardAddr(listenAddr) {
			publicAddr = listenAddr
		} else {
			fmt.Fprintln(os.Stderr, "Error: si relay escucha en 0.0.0.0 debes indicar --public host:puerto")
			return 1
		}
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error levantando relay: %v\n", err)
		return 1
	}
	defer ln.Close()

	hub := &relayHub{
		publicAddr: publicAddr,
		rooms:      make(map[string]*relayRoom),
		maxPerIP:   maxPerIP,
		maxTotal:   maxTotal,
		ipCounts:   make(map[string]int),
		active:     make(map[net.Conn]struct{}),
	}

	fmt.Printf("Relay escuchando en %s\n", listenAddr)
	fmt.Printf("Relay publico en %s\n", publicAddr)
	if maxPerIP > 0 {
		fmt.Printf("Limite por IP: %d conexiones TCP simultaneas\n", maxPerIP)
	}
	if maxTotal > 0 {
		fmt.Printf("Limite global: %d conexiones TCP simultaneas\n", maxTotal)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, relayShutdownSignals()...)
	go func() {
		<-sigCh
		logx.Info("relay server shutdown signal")
		_ = ln.Close()
		hub.shutdownAllConns()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if acceptShouldStop(err) {
				signal.Stop(sigCh)
				hub.shutdownAllConns()
				hub.wg.Wait()
				fmt.Println("Relay detenido.")
				return 0
			}
			logx.Warn("relay accept", "err", err)
			time.Sleep(acceptErrBackoff)
			continue
		}
		logx.Debug("relay connection accepted", "remote", conn.RemoteAddr().String())
		ipKey, ok := hub.tryEnter(conn.RemoteAddr())
		if !ok {
			logx.Warn("relay connection rejected", "reason", "limit", "remote", conn.RemoteAddr().String())
			_ = conn.Close()
			continue
		}
		hub.wg.Add(1)
		go func(c net.Conn, ip string) {
			defer hub.wg.Done()
			defer hub.leave(ip)
			hub.addActive(c)
			defer hub.removeActive(c)
			hub.handleConn(c)
		}(conn, ipKey)
	}
}

func relayShutdownSignals() []os.Signal {
	if runtime.GOOS == "windows" {
		return []os.Signal{os.Interrupt}
	}
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func (h *relayHub) tryEnter(addr net.Addr) (ip string, ok bool) {
	ip = relayPeerIP(addr)
	h.ipMu.Lock()
	defer h.ipMu.Unlock()
	if h.maxTotal > 0 && h.totalConns >= h.maxTotal {
		return ip, false
	}
	if h.maxPerIP > 0 {
		if h.ipCounts[ip] >= h.maxPerIP {
			return ip, false
		}
	}
	h.totalConns++
	h.ipCounts[ip]++
	return ip, true
}

func (h *relayHub) leave(ip string) {
	h.ipMu.Lock()
	defer h.ipMu.Unlock()
	if h.totalConns > 0 {
		h.totalConns--
	}
	n := h.ipCounts[ip]
	if n <= 1 {
		delete(h.ipCounts, ip)
	} else {
		h.ipCounts[ip] = n - 1
	}
}

func (h *relayHub) addActive(c net.Conn) {
	h.connMu.Lock()
	h.active[c] = struct{}{}
	h.connMu.Unlock()
}

func (h *relayHub) removeActive(c net.Conn) {
	h.connMu.Lock()
	delete(h.active, c)
	h.connMu.Unlock()
}

func (h *relayHub) shutdownAllConns() {
	h.connMu.Lock()
	for c := range h.active {
		_ = c.Close()
	}
	h.active = make(map[net.Conn]struct{})
	h.connMu.Unlock()
}

func (h *relayHub) handleConn(conn net.Conn) {
	defer conn.Close()

	writerDone := make(chan struct{})
	client := &serverClient{conn: conn, send: make(chan wireMessage, clientSendChanCap)}
	joined := false
	var room *relayRoom

	defer func() {
		if !joined {
			close(client.send)
			<-writerDone
			return
		}
		room.removeClient(client)
		logx.Info("relay room leave", "room", room.id, "user", client.name)
		room.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s salio", client.name), At: time.Now().Unix()}, nil)
		if room.clientCount() == 0 {
			h.deleteRoom(room.id)
		}
		close(client.send)
		<-writerDone
	}()

	go func() {
		defer close(writerDone)
		for msg := range client.send {
			if err := writeMessage(conn, msg); err != nil {
				return
			}
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, scannerTokenInitial), scannerTokenMax)

	if err := conn.SetReadDeadline(time.Now().Add(handshakeReadTimeout)); err != nil {
		logx.Debug("relay handshake deadline", "err", err)
	}
	if !scanner.Scan() {
		_ = conn.SetReadDeadline(time.Time{})
		if err := scanner.Err(); err != nil {
			logx.Debug("relay handshake scan", "err", err)
		}
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	first, err := decodeMessage(scanner.Text())
	if err != nil {
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "mensaje inicial invalido", At: time.Now().Unix()}, "handshake")
		return
	}

	name := strings.TrimSpace(first.From)
	if name == "" {
		name = "anon"
	}

	switch first.Type {
	case msgTypeCreate:
		room, err = h.createRoom(first.Text)
		if err != nil {
			enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: err.Error(), At: time.Now().Unix()}, "handshake")
			return
		}
		client.name = room.uniqueName(name)
		room.addClient(client)
		joined = true
		logx.Info("relay room created", "room", room.id, "user", client.name)
		enqueueClientWire(client, wireMessage{Type: msgTypeWelcome, Text: "Sala relay creada", Code: room.code, Peers: room.peerNames(), Assigned: client.name, Host: room.currentHostName(), At: time.Now().Unix()}, "welcome")
	case msgTypeJoin:
		room = h.getRoom(first.Room)
		if room == nil {
			enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "sala inexistente", At: time.Now().Unix()}, "handshake")
			return
		}
		if room.passwordHash != hashPassword(first.Text) {
			enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "password incorrecta", At: time.Now().Unix()}, "handshake")
			return
		}
		if room.isBannedName(name) {
			enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "estas baneado de esta sala", At: time.Now().Unix()}, "handshake")
			return
		}
		client.name = room.uniqueName(name)
		room.addClient(client)
		joined = true
		logx.Info("relay room join", "room", room.id, "user", client.name)
		enqueueClientWire(client, wireMessage{Type: msgTypeWelcome, Text: "Conectado a la sala relay", Code: room.code, Peers: room.peerNames(), Assigned: client.name, Host: room.currentHostName(), At: time.Now().Unix()}, "welcome")
		room.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s se unio", client.name), At: time.Now().Unix()}, client)
	default:
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "handshake invalido", At: time.Now().Unix()}, "handshake")
		return
	}

	decodeFails := 0
	for scanner.Scan() {
		msg, err := decodeMessage(scanner.Text())
		if err != nil {
			decodeFails++
			if decodeFails >= maxDecodeFailures {
				logx.Warn("relay decode failures closing", "remote", conn.RemoteAddr().String())
				return
			}
			continue
		}
		decodeFails = 0
		if msg.Type != msgTypeFileChunk && !client.allowMessage() {
			enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "rate limit excedido", At: time.Now().Unix()}, "rate_limit")
			continue
		}
		if msg.Type != msgTypePeersReq && msg.Type != msgTypeDiagReq && msg.Type != msgTypeQuit && msg.Type != msgTypeModCmd {
			if room.isMuted(client.name) {
				enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "estas silenciado por el host", At: time.Now().Unix()}, "muted")
				continue
			}
		}
		switch msg.Type {
		case msgTypeChat:
			text := strings.TrimSpace(msg.Text)
			if text == "" {
				continue
			}
			room.broadcast(wireMessage{Type: msgTypeChat, From: client.name, Text: text, At: time.Now().Unix()}, nil)
		case msgTypePeersReq:
			enqueueClientWire(client, wireMessage{Type: msgTypePeers, Peers: room.peerNames(), At: time.Now().Unix()}, "peers")
		case msgTypeDiagReq:
			enqueueClientWire(client, wireMessage{Type: msgTypeDiag, State: "connected", RTTMs: 0, RelayUsed: true, NATEst: "desconocido", At: time.Now().Unix()}, "diag")
		case msgTypePrivate:
			if strings.TrimSpace(msg.To) != "" && strings.TrimSpace(msg.Text) != "" {
				room.sendPrivate(client, msg.To, msg.Text)
			}
		case msgTypeReaction:
			if isReaction(msg.Text) {
				room.broadcast(wireMessage{Type: msgTypeReaction, From: client.name, Text: msg.Text, At: time.Now().Unix()}, nil)
			}
		case msgTypeFile:
			if strings.TrimSpace(msg.FileName) != "" && int64(len(msg.FilePayload)) <= maxFileBytes*2 {
				room.broadcast(wireMessage{Type: msgTypeFile, From: client.name, FileName: msg.FileName, FilePayload: msg.FilePayload, At: time.Now().Unix()}, nil)
			}
		case msgTypeFileChunk:
			if validFileChunk(msg) {
				msg.From = client.name
				msg.At = time.Now().Unix()
				room.broadcast(msg, nil)
			}
		case msgTypeQuit:
			return
		case msgTypeModCmd:
			room.handleModerationCommand(client, msg.Text)
		}
	}
	if err := scanner.Err(); err != nil {
		logx.Debug("relay scanner exit", "err", err)
	}
}

func (h *relayHub) createRoom(password string) (*relayRoom, error) {
	roomID, err := randomID(8)
	if err != nil {
		return nil, err
	}
	code, err := BuildRelayRoomCode(h.publicAddr, roomID)
	if err != nil {
		return nil, err
	}
	room := &relayRoom{
		id:           roomID,
		passwordHash: hashPassword(password),
		code:         code,
		clients:      make(map[*serverClient]struct{}),
		muted:        make(map[string]bool),
		banned:       make(map[string]bool),
	}
	h.mu.Lock()
	h.rooms[roomID] = room
	h.mu.Unlock()
	return room, nil
}

func (h *relayHub) getRoom(id string) *relayRoom {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[id]
}

func (h *relayHub) deleteRoom(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, id)
}

func (r *relayRoom) addClient(c *serverClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.clients) == 0 {
		c.isHost = true
	}
	c.joinedAt = time.Now()
	r.clients[c] = struct{}{}
}

func (r *relayRoom) removeClient(c *serverClient) {
	var newHost string
	r.mu.Lock()
	wasHost := c.isHost
	delete(r.clients, c)
	if wasHost {
		successor := pickSuccessorHost(r.clients)
		if successor != nil {
			successor.isHost = true
			newHost = successor.name
			enqueueClientWire(successor, wireMessage{Type: msgTypeSystem, Text: "Ahora eres host de la sala", At: time.Now().Unix()}, "host_transfer")
		}
	}
	r.mu.Unlock()
	if newHost != "" {
		r.broadcast(wireMessage{Type: msgTypeHostUpdate, Host: newHost, At: time.Now().Unix()}, nil)
	}
}

func (r *relayRoom) currentHostName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.clients {
		if c.isHost {
			return c.name
		}
	}
	return ""
}

func (r *relayRoom) clientCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients)
}

func (r *relayRoom) peerNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	peers := make([]string, 0, len(r.clients))
	for c := range r.clients {
		peers = append(peers, c.name)
	}
	sort.Strings(peers)
	return peers
}

func (r *relayRoom) uniqueName(base string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	used := map[string]bool{}
	for c := range r.clients {
		used[c.name] = true
	}
	return allocUniqueNick(base, func(n string) bool { return used[n] })
}

func (r *relayRoom) broadcast(msg wireMessage, except *serverClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.clients {
		if c == except {
			continue
		}
		enqueueClientWire(c, msg, "broadcast")
	}
}

func (r *relayRoom) sendPrivate(from *serverClient, toName, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	to, amb := findClientInSet(r.clients, toName)
	if amb {
		enqueueClientWire(from, wireMessage{Type: msgTypeError, Text: "varios usuarios coinciden con ese nick; usa el nombre exacto", At: time.Now().Unix()}, "private")
		return
	}
	if to == nil {
		enqueueClientWire(from, wireMessage{Type: msgTypeError, Text: fmt.Sprintf("usuario %q no encontrado", toName), At: time.Now().Unix()}, "private")
		return
	}
	msg := wireMessage{Type: msgTypePrivate, From: from.name, To: to.name, Text: text, At: time.Now().Unix()}
	enqueueClientWire(from, msg, "private_echo")
	if to != from {
		enqueueClientWire(to, msg, "private")
	}
}

func (r *relayRoom) isMuted(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.muted[normalizeNickKey(name)]
}

func (r *relayRoom) isBannedName(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.banned[normalizeNickKey(name)]
}

func (r *relayRoom) findClientByName(name string) (match *serverClient, ambiguous bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return findClientInSet(r.clients, name)
}

func (r *relayRoom) handleModerationCommand(sender *serverClient, line string) {
	if !sender.isHost {
		enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "solo el host puede moderar", At: time.Now().Unix()}, "mod")
		return
	}
	cmd, user, ok := parseModerationCommand(line)
	if !ok {
		enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "uso: /kick|/mute|/unmute|/ban <usuario>", At: time.Now().Unix()}, "mod")
		return
	}
	target, amb := r.findClientByName(user)
	switch cmd {
	case "/mute":
		r.mu.Lock()
		r.muted[normalizeNickKey(user)] = true
		r.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeSystem, Text: "Has sido silenciado por el host", At: time.Now().Unix()}, "mod")
		}
		r.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s fue silenciado por el host", user), At: time.Now().Unix()}, nil)
	case "/unmute":
		r.mu.Lock()
		delete(r.muted, normalizeNickKey(user))
		r.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeSystem, Text: "Ya no estas silenciado", At: time.Now().Unix()}, "mod")
		}
		r.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s ya puede escribir", user), At: time.Now().Unix()}, nil)
	case "/kick":
		if amb {
			enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "varios usuarios coinciden con ese nick; usa el nombre exacto", At: time.Now().Unix()}, "mod")
			return
		}
		if target == nil {
			enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "usuario no encontrado", At: time.Now().Unix()}, "mod")
			return
		}
		enqueueClientWire(target, wireMessage{Type: msgTypeError, Text: "expulsado por el host", At: time.Now().Unix()}, "kick")
		_ = target.conn.Close()
	case "/ban":
		if amb {
			enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "varios usuarios coinciden con ese nick; usa el nombre exacto", At: time.Now().Unix()}, "mod")
			return
		}
		r.mu.Lock()
		r.banned[normalizeNickKey(user)] = true
		delete(r.muted, normalizeNickKey(user))
		r.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeError, Text: "baneado por el host", At: time.Now().Unix()}, "ban")
			_ = target.conn.Close()
		}
		r.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s fue baneado por el host", user), At: time.Now().Unix()}, nil)
	}
}

func runRelayClient(relayAddr, roomID, name, password, roomCode string, create bool, codeProtocol string) int {
	conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error conectando: %v\n", err)
		logx.Error("relay dial failed", "relay", relayAddr, "err", err)
		return 1
	}
	defer conn.Close()

	firstType := msgTypeJoin
	if create {
		firstType = msgTypeCreate
	}

	first := wireMessage{Type: firstType, Room: roomID, From: name, Text: password, At: time.Now().Unix()}
	if err := writeMessage(conn, first); err != nil {
		fmt.Fprintf(os.Stderr, "Error enviando handshake: %v\n", err)
		return 1
	}

	done := make(chan struct{})
	errCh := make(chan error, 1)
	codeCh := make(chan string, 1)
	ht := &roomHostTracker{}
	myNick := strings.TrimSpace(name)
	if myNick == "" {
		myNick = "guest"
	}
	var nickMu sync.RWMutex

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, scannerTokenInitial), scannerTokenMax)
		decodeFails := 0
		for scanner.Scan() {
			msg, err := decodeMessage(scanner.Text())
			if err != nil {
				decodeFails++
				if decodeFails >= maxDecodeFailures {
					logx.Warn("relay client decode failures closing", "remote", conn.RemoteAddr().String())
					return
				}
				continue
			}
			decodeFails = 0
			if msg.Type == msgTypeWelcome && strings.TrimSpace(msg.Assigned) != "" {
				nickMu.Lock()
				myNick = strings.TrimSpace(msg.Assigned)
				nickMu.Unlock()
			}
			if msg.Type == msgTypeWelcome && strings.TrimSpace(msg.Code) != "" {
				if create && strings.TrimSpace(codeProtocol) != "" {
					msg.Code = RewriteCodeProtocol(msg.Code, codeProtocol)
				}
				select {
				case codeCh <- msg.Code:
				default:
				}
			}
			nickMu.RLock()
			currentNick := myNick
			nickMu.RUnlock()
			if shouldSkipOwnEcho(msg, currentNick) {
				continue
			}
			renderWireMessage(msg, currentNick, ht)
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	fmt.Println("Escribe mensajes y Enter para enviar")
	fmt.Println("Comandos: /code /peers /diag /clear /history !! /kick|/mute|/unmute|/ban @usuario mensaje | /msg u texto | /send archivo /quit /help")

	stdinLines := make(chan string, stdinLineChanCap)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			select {
			case stdinLines <- line:
			case <-done:
				return
			}
		}
		if err := sc.Err(); err != nil {
			logx.Debug("relay client stdin", "err", err)
		}
		close(stdinLines)
	}()

	localLimiter := &inputRateLimiter{tokens: 3, max: 3, refillPerSec: 0.55, last: time.Now()}
	history := make([]string, 0, 50)
	for {
	codes:
		for {
			select {
			case c := <-codeCh:
				roomCode = c
				_ = SaveLastRoomCode(c)
				if create {
					clipboard.AnnounceRoomCode(c)
				}
				logx.Info("relay room code received", "protocol", codeProtocol)
			default:
				break codes
			}
		}

		select {
		case <-done:
			select {
			case err := <-errCh:
				fmt.Fprintf(os.Stderr, "Conexion cerrada: %v\n", err)
				return 2
			default:
			}
			return 0
		case line, ok := <-stdinLines:
			if !ok {
				_ = writeMessage(conn, wireMessage{Type: msgTypeQuit, At: time.Now().Unix()})
				return 0
			}
			if line == "" {
				continue
			}
			if line == "!!" {
				if len(history) == 0 {
					fmt.Println("Historial vacio")
					continue
				}
				line = history[len(history)-1]
				fmt.Printf("repite: %s\n", line)
			}
			history = appendHistory(history, line, 40)

			if isReaction(line) {
				if !localLimiter.allow() {
					printRateLimitLocal()
					continue
				}
				_ = writeMessage(conn, wireMessage{Type: msgTypeReaction, Text: line, At: time.Now().Unix()})
				continue
			}

			if to, txt, ok := parseAtMention(line); ok {
				if !localLimiter.allow() {
					printRateLimitLocal()
					continue
				}
				_ = writeMessage(conn, wireMessage{Type: msgTypePrivate, To: to, Text: txt, At: time.Now().Unix()})
				continue
			}

			if strings.HasPrefix(line, "/") {
				switch line {
				case "/quit":
					_ = writeMessage(conn, wireMessage{Type: msgTypeQuit, At: time.Now().Unix()})
					return 0
				case "/peers":
					_ = writeMessage(conn, wireMessage{Type: msgTypePeersReq, At: time.Now().Unix()})
				case "/diag":
					_ = writeMessage(conn, wireMessage{Type: msgTypeDiagReq, At: time.Now().Unix()})
				case "/code":
					if strings.TrimSpace(roomCode) != "" {
						fmt.Printf("Codigo de sala: %s\n", roomCode)
					} else {
						fmt.Println("No hay codigo disponible en esta sesion")
					}
				case "/help":
					fmt.Println("Comandos: /code /peers /diag /clear /history !! /kick|/mute|/unmute|/ban @usuario mensaje | /msg u texto | /send archivo /quit /help")
				case "/clear":
					clearConsole()
				case "/history":
					printInputHistory(history)
				default:
					if isModerationCommandLine(line) {
						if !localLimiter.allow() {
							printRateLimitLocal()
							continue
						}
						_ = writeMessage(conn, wireMessage{Type: msgTypeModCmd, Text: line, At: time.Now().Unix()})
						continue
					}
					if strings.HasPrefix(line, "/msg ") {
						to, text, ok := parsePrivateCommand(line)
						if !ok {
							fmt.Println("Uso: /msg <usuario> <texto>")
							continue
						}
						if !localLimiter.allow() {
							printRateLimitLocal()
							continue
						}
						_ = writeMessage(conn, wireMessage{Type: msgTypePrivate, To: to, Text: text, At: time.Now().Unix()})
					} else if strings.HasPrefix(line, "/send ") {
						if !localLimiter.allow() {
							printRateLimitLocal()
							continue
						}
						if err := sendFileChunks(conn, strings.TrimSpace(strings.TrimPrefix(line, "/send "))); err != nil {
							fmt.Fprintf(os.Stderr, "Error /send: %v\n", err)
						}
					} else {
						fmt.Println("Comando no reconocido. Usa /help")
					}
				}
				continue
			}

			if !localLimiter.allow() {
				printRateLimitLocal()
				continue
			}
			at := time.Now().Unix()
			if err := writeMessage(conn, wireMessage{Type: msgTypeChat, Text: line, At: at}); err != nil {
				fmt.Fprintf(os.Stderr, "Error enviando mensaje: %v\n", err)
				return 1
			}
			nickMu.RLock()
			currentNick := myNick
			nickMu.RUnlock()
			renderWireMessage(wireMessage{Type: msgTypeChat, From: currentNick, Text: line, At: at}, currentNick, ht)
		}
	}
}
