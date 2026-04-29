package chat

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"brinco-cli/internal/clipboard"
	"brinco-cli/internal/logx"
	"brinco-cli/internal/roomproto"
)

type relayHub struct {
	publicAddr string
	ln         net.Listener

	mu    sync.Mutex
	rooms map[string]*relayRoom
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
	logx.Info("relay create start", "relay", relayAddr, "protocol", protocol)
	fmt.Printf("Conectando al relay %s...\n", relayAddr)
	return runRelayClient(relayAddr, "", name, password, "", true, protocol)
}

func RunRelayServer(listenAddr, publicAddr string) int {
	logx.Info("relay server start", "listen", listenAddr, "public", publicAddr)
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
		ln:         ln,
		rooms:      make(map[string]*relayRoom),
	}

	fmt.Printf("Relay escuchando en %s\n", listenAddr)
	fmt.Printf("Relay publico en %s\n", publicAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Relay accept error: %v\n", err)
			logx.Error("relay accept error", "err", err)
			return 1
		}
		logx.Debug("relay connection accepted", "remote", conn.RemoteAddr().String())
		go hub.handleConn(conn)
	}
}

func (h *relayHub) handleConn(conn net.Conn) {
	defer conn.Close()

	writerDone := make(chan struct{})
	client := &serverClient{conn: conn, send: make(chan wireMessage, 32)}
	go func() {
		defer close(writerDone)
		for msg := range client.send {
			if err := writeMessage(conn, msg); err != nil {
				return
			}
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	if !scanner.Scan() {
		close(client.send)
		<-writerDone
		return
	}

	first, err := decodeMessage(scanner.Text())
	if err != nil {
		sendDirect(client, wireMessage{Type: msgTypeError, Text: "mensaje inicial invalido", At: time.Now().Unix()})
		close(client.send)
		<-writerDone
		return
	}

	name := strings.TrimSpace(first.From)
	if name == "" {
		name = "anon"
	}

	var room *relayRoom
	switch first.Type {
	case msgTypeCreate:
		room, err = h.createRoom(first.Text)
		if err != nil {
			sendDirect(client, wireMessage{Type: msgTypeError, Text: err.Error(), At: time.Now().Unix()})
			close(client.send)
			<-writerDone
			return
		}
		client.name = room.uniqueName(name)
		room.addClient(client)
		logx.Info("relay room created", "room", room.id, "user", client.name)
		sendDirect(client, wireMessage{Type: msgTypeWelcome, Text: "Sala relay creada", Code: room.code, Peers: room.peerNames(), Assigned: client.name, Host: room.currentHostName(), At: time.Now().Unix()})
	case msgTypeJoin:
		room = h.getRoom(first.Room)
		if room == nil {
			sendDirect(client, wireMessage{Type: msgTypeError, Text: "sala inexistente", At: time.Now().Unix()})
			close(client.send)
			<-writerDone
			return
		}
		if room.passwordHash != hashPassword(first.Text) {
			sendDirect(client, wireMessage{Type: msgTypeError, Text: "password incorrecta", At: time.Now().Unix()})
			close(client.send)
			<-writerDone
			return
		}
		if room.isBannedName(name) {
			sendDirect(client, wireMessage{Type: msgTypeError, Text: "estas baneado de esta sala", At: time.Now().Unix()})
			close(client.send)
			<-writerDone
			return
		}
		client.name = room.uniqueName(name)
		room.addClient(client)
		logx.Info("relay room join", "room", room.id, "user", client.name)
		sendDirect(client, wireMessage{Type: msgTypeWelcome, Text: "Conectado a la sala relay", Code: room.code, Peers: room.peerNames(), Assigned: client.name, Host: room.currentHostName(), At: time.Now().Unix()})
		room.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s se unio", client.name), At: time.Now().Unix()}, client)
	default:
		sendDirect(client, wireMessage{Type: msgTypeError, Text: "handshake invalido", At: time.Now().Unix()})
		close(client.send)
		<-writerDone
		return
	}

	for scanner.Scan() {
		msg, err := decodeMessage(scanner.Text())
		if err != nil {
			continue
		}
		if !client.allowMessage() {
			sendDirect(client, wireMessage{Type: msgTypeError, Text: "rate limit excedido", At: time.Now().Unix()})
			continue
		}
		if msg.Type != msgTypePeersReq && msg.Type != msgTypeDiagReq && msg.Type != msgTypeQuit && msg.Type != msgTypeModCmd {
			if room.isMuted(client.name) {
				sendDirect(client, wireMessage{Type: msgTypeError, Text: "estas silenciado por el host", At: time.Now().Unix()})
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
			sendDirect(client, wireMessage{Type: msgTypePeers, Peers: room.peerNames(), At: time.Now().Unix()})
		case msgTypeDiagReq:
			sendDirect(client, wireMessage{Type: msgTypeDiag, State: "connected", RTTMs: 0, RelayUsed: true, NATEst: "desconocido", At: time.Now().Unix()})
		case msgTypePrivate:
			if strings.TrimSpace(msg.To) != "" && strings.TrimSpace(msg.Text) != "" {
				room.sendPrivate(client, msg.To, msg.Text)
			}
		case msgTypeReaction:
			if isReaction(msg.Text) {
				room.broadcast(wireMessage{Type: msgTypeReaction, From: client.name, Text: msg.Text, At: time.Now().Unix()}, nil)
			}
		case msgTypeFile:
			if strings.TrimSpace(msg.FileName) != "" && len(msg.FilePayload) <= 2_200_000 {
				room.broadcast(wireMessage{Type: msgTypeFile, From: client.name, FileName: msg.FileName, FilePayload: msg.FilePayload, At: time.Now().Unix()}, nil)
			}
		case msgTypeQuit:
			goto cleanup
		case msgTypeModCmd:
			room.handleModerationCommand(client, msg.Text)
		}
	}

cleanup:
	room.removeClient(client)
	logx.Info("relay room leave", "room", room.id, "user", client.name)
	room.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s salio", client.name), At: time.Now().Unix()}, nil)
	if room.clientCount() == 0 {
		h.deleteRoom(room.id)
	}
	close(client.send)
	<-writerDone
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
	r.clients[c] = struct{}{}
}

func (r *relayRoom) removeClient(c *serverClient) {
	var newHost string
	r.mu.Lock()
	wasHost := c.isHost
	delete(r.clients, c)
	if wasHost {
		for other := range r.clients {
			other.isHost = true
			newHost = other.name
			select {
			case other.send <- wireMessage{Type: msgTypeSystem, Text: "Ahora eres host de la sala", At: time.Now().Unix()}:
			default:
			}
			break
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
	if !used[base] {
		return base
	}
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s#%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
	return fmt.Sprintf("%s#x", base)
}

func (r *relayRoom) broadcast(msg wireMessage, except *serverClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.clients {
		if c == except {
			continue
		}
		select {
		case c.send <- msg:
		default:
		}
	}
}

func (r *relayRoom) sendPrivate(from *serverClient, toName, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var to *serverClient
	for c := range r.clients {
		if c.name == strings.TrimSpace(toName) {
			to = c
			break
		}
	}
	if to == nil {
		select {
		case from.send <- wireMessage{Type: msgTypeError, Text: fmt.Sprintf("usuario %q no encontrado", toName), At: time.Now().Unix()}:
		default:
		}
		return
	}
	msg := wireMessage{Type: msgTypePrivate, From: from.name, To: to.name, Text: text, At: time.Now().Unix()}
	select {
	case from.send <- msg:
	default:
	}
	if to != from {
		select {
		case to.send <- msg:
		default:
		}
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

func (r *relayRoom) findClientByName(name string) *serverClient {
	r.mu.Lock()
	defer r.mu.Unlock()
	target := strings.TrimSpace(name)
	for c := range r.clients {
		if c.name == target {
			return c
		}
	}
	return nil
}

func (r *relayRoom) handleModerationCommand(sender *serverClient, line string) {
	if !sender.isHost {
		sendDirect(sender, wireMessage{Type: msgTypeError, Text: "solo el host puede moderar", At: time.Now().Unix()})
		return
	}
	cmd, user, ok := parseModerationCommand(line)
	if !ok {
		sendDirect(sender, wireMessage{Type: msgTypeError, Text: "uso: /kick|/mute|/unmute|/ban <usuario>", At: time.Now().Unix()})
		return
	}
	target := r.findClientByName(user)
	switch cmd {
	case "/mute":
		r.mu.Lock()
		r.muted[normalizeNickKey(user)] = true
		r.mu.Unlock()
		if target != nil {
			sendDirect(target, wireMessage{Type: msgTypeSystem, Text: "Has sido silenciado por el host", At: time.Now().Unix()})
		}
		r.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s fue silenciado por el host", user), At: time.Now().Unix()}, nil)
	case "/unmute":
		r.mu.Lock()
		delete(r.muted, normalizeNickKey(user))
		r.mu.Unlock()
		if target != nil {
			sendDirect(target, wireMessage{Type: msgTypeSystem, Text: "Ya no estas silenciado", At: time.Now().Unix()})
		}
		r.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s ya puede escribir", user), At: time.Now().Unix()}, nil)
	case "/kick":
		if target == nil {
			sendDirect(sender, wireMessage{Type: msgTypeError, Text: "usuario no encontrado", At: time.Now().Unix()})
			return
		}
		sendDirect(target, wireMessage{Type: msgTypeError, Text: "expulsado por el host", At: time.Now().Unix()})
		_ = target.conn.Close()
	case "/ban":
		r.mu.Lock()
		r.banned[normalizeNickKey(user)] = true
		delete(r.muted, normalizeNickKey(user))
		r.mu.Unlock()
		if target != nil {
			sendDirect(target, wireMessage{Type: msgTypeError, Text: "baneado por el host", At: time.Now().Unix()})
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
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			msg, err := decodeMessage(scanner.Text())
			if err != nil {
				continue
			}
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

	stdin := bufio.NewScanner(os.Stdin)
	localLimiter := &inputRateLimiter{tokens: 3, max: 3, refillPerSec: 0.55, last: time.Now()}
	history := make([]string, 0, 50)
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
		default:
		}

		if !stdin.Scan() {
			_ = writeMessage(conn, wireMessage{Type: msgTypeQuit, At: time.Now().Unix()})
			return 0
		}
		line := strings.TrimSpace(stdin.Text())
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
					msg, err := buildFileMessage(strings.TrimSpace(strings.TrimPrefix(line, "/send ")))
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error /send: %v\n", err)
					} else {
						if !localLimiter.allow() {
							printRateLimitLocal()
							continue
						}
						_ = writeMessage(conn, msg)
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
