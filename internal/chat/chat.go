package chat

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"brinco-cli/internal/logx"
	"brinco-cli/internal/roomproto"
)

const (
	msgTypeCreate   = "create"
	msgTypeJoin     = "join"
	msgTypeWelcome  = "welcome"
	msgTypeChat     = "chat"
	msgTypeSystem   = "system"
	msgTypeError    = "error"
	msgTypePeersReq = "peers_req"
	msgTypePeers    = "peers"
	msgTypeQuit     = "quit"
	msgTypePrivate  = "private"
	msgTypeReaction = "reaction"
	msgTypeFile     = "file"
	msgTypeDiagReq  = "diag_req"
	msgTypeDiag     = "diag"

	roomModeDirect     = "direct"
	roomModeRelay      = "relay"
	roomModeGuaranteed = "guaranteed"

	lastRoomCodeFile = "brinco-last-room-code.txt"
)

var (
	nickColorCodes   = []string{
		"38;5;220", "38;5;226", "38;5;33", "38;5;39", "38;5;45", "38;5;51", "38;5;50", "38;5;49",
		"38;5;48", "38;5;47", "38;5;46", "38;5;82", "38;5;118", "38;5;154",
		"38;5;190", "38;5;214", "38;5;208", "38;5;202",
		"38;5;196", "38;5;197", "38;5;198", "38;5;199", "38;5;200", "38;5;201",
		"38;5;129", "38;5;135", "38;5;141", "38;5;147", "38;5;153", "38;5;159",
		"38;5;123", "38;5;87", "38;5;81", "38;5;75", "38;5;69", "38;5;63",
		"38;5;57", "38;5;93", "38;5;99", "38;5;105", "38;5;111", "38;5;117",
	}
	nickColorMu      sync.Mutex
	nickColorByName  = map[string]string{}
	nextNickColorIdx int
)

type roomCodePayload struct {
	Mode  string `json:"mode,omitempty"`
	Addr  string `json:"addr,omitempty"`
	Relay string `json:"relay,omitempty"`
	Room  string `json:"room"`
}

type wireMessage struct {
	Type        string   `json:"type"`
	Room        string   `json:"room,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Text        string   `json:"text,omitempty"`
	Peers       []string `json:"peers,omitempty"`
	Code        string   `json:"code,omitempty"`
	FileName    string   `json:"fileName,omitempty"`
	FilePayload string   `json:"filePayload,omitempty"`
	State       string   `json:"state,omitempty"`
	RTTMs       int64    `json:"rttMs,omitempty"`
	RelayUsed   bool     `json:"relayUsed,omitempty"`
	NATEst      string   `json:"natEst,omitempty"`
	// Assigned es el nombre final en sala (tras uniqueName); solo en welcome.
	Assigned string `json:"assigned,omitempty"`
	At       int64  `json:"at"`
}

type serverClient struct {
	name   string
	conn   net.Conn
	send   chan wireMessage
	last   time.Time
	tokens float64
}

type roomServer struct {
	roomID       string
	passwordHash string
	code         string

	ln net.Listener

	mu      sync.Mutex
	clients map[*serverClient]struct{}
}

func RunCreate(name, listenAddr, publicAddr, password string) int {
	logx.Info("chat create start", "mode", roomModeDirect, "listen", listenAddr)
	if strings.TrimSpace(name) == "" {
		name = "host"
	}

	if strings.TrimSpace(publicAddr) == "" {
		if !isWildcardAddr(listenAddr) {
			publicAddr = listenAddr
		} else {
			fmt.Fprintln(os.Stderr, "Error: si usas 0.0.0.0 debes indicar --public host:puerto")
			return 1
		}
	}

	roomID, err := randomID(8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando room id: %v\n", err)
		return 1
	}

	code, err := BuildRoomCode(publicAddr, roomID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando codigo de sala: %v\n", err)
		return 1
	}

	srv, err := startServer(listenAddr, roomID, password, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando sala: %v\n", err)
		return 1
	}
	defer srv.Close()

	_ = SaveLastRoomCode(code)

	fmt.Println("Sala creada")
	fmt.Printf("Codigo de sala: %s\n", code)
	fmt.Println("Comparte este codigo con tus peers")
	logx.Info("chat sala creada", "mode", roomModeDirect, "room", roomID)

	dialAddr := dialAddrForHost(listenAddr)
	return runClient(dialAddr, roomID, name, password, code)
}

func RunJoin(name, code, password string) int {
	logx.Info("chat join start")
	if strings.TrimSpace(code) == "" {
		fmt.Fprintln(os.Stderr, "Error: --code es obligatorio")
		return 1
	}
	if strings.TrimSpace(name) == "" {
		name = "guest"
	}

	payload, err := ParseRoomCodeDetailed(code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parseando codigo: %v\n", err)
		return 1
	}

	addr := payload.Addr
	if strings.TrimSpace(payload.Relay) != "" {
		addr = payload.Relay
	}
	roomID := payload.Room
	mode := strings.TrimSpace(payload.Mode)
	if mode == "" {
		mode = roomModeDirect
	}

	fmt.Printf("Conectando a %s...\n", addr)
	if mode == roomModeRelay || mode == roomModeGuaranteed {
		return runWithReconnect(func() int {
			return runRelayClient(addr, roomID, name, password, code, false, mode)
		})
	}
	return runWithReconnect(func() int {
		return runClient(addr, roomID, name, password, code)
	})
}

func BuildRoomCode(addr, room string) (string, error) {
	payload := roomCodePayload{Mode: roomModeDirect, Addr: addr, Room: room}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(roomproto.ProtocolDirect, base64.RawURLEncoding.EncodeToString(raw)), nil
}

func BuildRelayRoomCode(relayAddr, room string) (string, error) {
	payload := roomCodePayload{Mode: roomModeRelay, Relay: relayAddr, Room: room}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(roomproto.ProtocolRelay, base64.RawURLEncoding.EncodeToString(raw)), nil
}

func RewriteCodeProtocol(code, protocol string) string {
	p, payload := roomproto.Unwrap(code)
	if p == "" {
		return roomproto.Wrap(protocol, code)
	}
	return roomproto.Wrap(protocol, payload)
}

func ParseRoomCodeDetailed(code string) (roomCodePayload, error) {
	protocol, encoded := roomproto.Unwrap(code)
	if protocol == roomproto.ProtocolP2P {
		return roomCodePayload{}, errors.New("codigo p2p no valido para chat directo")
	}
	if encoded == "" {
		encoded = strings.TrimSpace(code)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return roomCodePayload{}, err
	}

	var rc roomCodePayload
	if err := json.Unmarshal(raw, &rc); err != nil {
		return roomCodePayload{}, err
	}
	if rc.Room == "" {
		return roomCodePayload{}, errors.New("codigo invalido")
	}
	switch protocol {
	case roomproto.ProtocolRelay:
		rc.Mode = roomModeRelay
	case roomproto.ProtocolGuaranteed:
		rc.Mode = roomModeGuaranteed
	case roomproto.ProtocolDirect:
		rc.Mode = roomModeDirect
	default:
		if strings.TrimSpace(rc.Mode) == "" {
			rc.Mode = roomModeDirect
		}
	}
	if rc.Mode == roomModeRelay || rc.Mode == roomModeGuaranteed {
		if strings.TrimSpace(rc.Relay) == "" {
			return roomCodePayload{}, errors.New("codigo relay invalido")
		}
		return rc, nil
	}
	if strings.TrimSpace(rc.Addr) == "" {
		return roomCodePayload{}, errors.New("codigo directo invalido")
	}
	return rc, nil
}

func ParseRoomCode(code string) (addr string, room string, err error) {
	payload, err := ParseRoomCodeDetailed(code)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(payload.Relay) != "" {
		return payload.Relay, payload.Room, nil
	}
	return payload.Addr, payload.Room, nil
}

func SaveLastRoomCode(code string) error {
	path, err := roomCodePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(code)), 0o600)
}

func LoadLastRoomCode() (string, error) {
	path, err := roomCodePath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(string(raw))
	if code == "" {
		return "", errors.New("no hay codigo guardado")
	}
	return code, nil
}

func roomCodePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, lastRoomCodeFile), nil
}

func startServer(listenAddr, roomID, password, code string) (*roomServer, error) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	s := &roomServer{
		roomID:       roomID,
		passwordHash: hashPassword(password),
		code:         code,
		ln:           ln,
		clients:      make(map[*serverClient]struct{}),
	}
	go s.acceptLoop()
	return s, nil
}

func (s *roomServer) Close() {
	_ = s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		close(c.send)
		_ = c.conn.Close()
	}
}

func (s *roomServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *roomServer) handleConn(conn net.Conn) {
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

	join, err := decodeMessage(scanner.Text())
	if err != nil || join.Type != msgTypeJoin {
		sendDirect(client, wireMessage{Type: msgTypeError, Text: "handshake invalido", At: time.Now().Unix()})
		close(client.send)
		<-writerDone
		return
	}
	if join.Room != s.roomID {
		sendDirect(client, wireMessage{Type: msgTypeError, Text: "sala inexistente", At: time.Now().Unix()})
		close(client.send)
		<-writerDone
		return
	}
	if hashPassword(join.Text) != s.passwordHash {
		sendDirect(client, wireMessage{Type: msgTypeError, Text: "password incorrecta", At: time.Now().Unix()})
		close(client.send)
		<-writerDone
		return
	}

	name := strings.TrimSpace(join.From)
	if name == "" {
		name = "anon"
	}
	client.name = s.uniqueName(name)
	s.addClient(client)

	sendDirect(client, wireMessage{
		Type:     msgTypeWelcome,
		Text:     "Conectado a la sala",
		Code:     s.code,
		Peers:    s.peerNames(),
		Assigned: client.name,
		At:       time.Now().Unix(),
	})
	s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s se unio", client.name), At: time.Now().Unix()}, nil)

	for scanner.Scan() {
		msg, err := decodeMessage(scanner.Text())
		if err != nil {
			continue
		}
		s.handleClientMessage(client, msg)
		if msg.Type == msgTypeQuit {
			break
		}
	}

	s.removeClient(client)
	s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s salio", client.name), At: time.Now().Unix()}, nil)
	close(client.send)
	<-writerDone
}

func (s *roomServer) handleClientMessage(c *serverClient, msg wireMessage) {
	if !c.allowMessage() {
		sendDirect(c, wireMessage{Type: msgTypeError, Text: "rate limit excedido", At: time.Now().Unix()})
		return
	}
	switch msg.Type {
	case msgTypeChat:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		s.broadcast(wireMessage{Type: msgTypeChat, From: c.name, Text: text, At: time.Now().Unix()}, nil)
	case msgTypePeersReq:
		sendDirect(c, wireMessage{Type: msgTypePeers, Peers: s.peerNames(), At: time.Now().Unix()})
	case msgTypeDiagReq:
		sendDirect(c, wireMessage{Type: msgTypeDiag, State: "connected", RTTMs: 0, RelayUsed: false, NATEst: "desconocido", At: time.Now().Unix()})
	case msgTypePrivate:
		if strings.TrimSpace(msg.To) == "" || strings.TrimSpace(msg.Text) == "" {
			return
		}
		s.sendPrivate(c, msg.To, msg.Text)
	case msgTypeReaction:
		if isReaction(msg.Text) {
			s.broadcast(wireMessage{Type: msgTypeReaction, From: c.name, Text: msg.Text, At: time.Now().Unix()}, nil)
		}
	case msgTypeFile:
		if strings.TrimSpace(msg.FileName) == "" || len(msg.FilePayload) > 2_200_000 {
			return
		}
		s.broadcast(wireMessage{Type: msgTypeFile, From: c.name, FileName: msg.FileName, FilePayload: msg.FilePayload, At: time.Now().Unix()}, nil)
	case msgTypeQuit:
		return
	}
}

func (c *serverClient) allowMessage() bool {
	now := time.Now()
	if c.last.IsZero() {
		c.last = now
		c.tokens = 12
	}
	c.tokens += now.Sub(c.last).Seconds() * 6
	if c.tokens > 12 {
		c.tokens = 12
	}
	c.last = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func (s *roomServer) addClient(c *serverClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
}

func (s *roomServer) removeClient(c *serverClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

func (s *roomServer) broadcast(msg wireMessage, except *serverClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c == except {
			continue
		}
		select {
		case c.send <- msg:
		default:
		}
	}
}

func (s *roomServer) sendPrivate(from *serverClient, toName, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var to *serverClient
	for c := range s.clients {
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

func (s *roomServer) peerNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	peers := make([]string, 0, len(s.clients))
	for c := range s.clients {
		peers = append(peers, c.name)
	}
	sort.Strings(peers)
	return peers
}

func (s *roomServer) uniqueName(base string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	used := map[string]bool{}
	for c := range s.clients {
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

func runClient(addr, roomID, name, password, roomCode string) int {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error conectando: %v\n", err)
		return 1
	}
	defer conn.Close()

	join := wireMessage{Type: msgTypeJoin, Room: roomID, From: name, Text: password, At: time.Now().Unix()}
	if err := writeMessage(conn, join); err != nil {
		fmt.Fprintf(os.Stderr, "Error enviando join: %v\n", err)
		return 1
	}

	done := make(chan struct{})
	errCh := make(chan error, 1)
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
			nickMu.RLock()
			currentNick := myNick
			nickMu.RUnlock()
			if shouldSkipOwnEcho(msg, currentNick) {
				continue
			}
			renderWireMessage(msg, currentNick)
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	fmt.Println("Escribe mensajes y Enter para enviar")
	fmt.Println("Comandos: /code /peers /diag /clear @usuario mensaje | /msg u texto | /send archivo /quit /help")

	stdin := bufio.NewScanner(os.Stdin)
	for {
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

		if isReaction(line) {
			_ = writeMessage(conn, wireMessage{Type: msgTypeReaction, Text: line, At: time.Now().Unix()})
			continue
		}

		if to, txt, ok := parseAtMention(line); ok {
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
				fmt.Println("Comandos: /code /peers /diag /clear @usuario mensaje | /msg u t | /send archivo /quit /help")
			case "/clear":
				clearConsole()
			default:
				if strings.HasPrefix(line, "/msg ") {
					to, text, ok := parsePrivateCommand(line)
					if !ok {
						fmt.Println("Uso: /msg <usuario> <texto>")
						continue
					}
					_ = writeMessage(conn, wireMessage{Type: msgTypePrivate, To: to, Text: text, At: time.Now().Unix()})
				} else if strings.HasPrefix(line, "/send ") {
					msg, err := buildFileMessage(strings.TrimSpace(strings.TrimPrefix(line, "/send ")))
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error /send: %v\n", err)
					} else {
						_ = writeMessage(conn, msg)
					}
				} else {
					fmt.Println("Comando no reconocido. Usa /help")
				}
			}
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
		renderWireMessage(wireMessage{Type: msgTypeChat, From: currentNick, Text: line, At: at}, currentNick)
	}
}

func sendDirect(c *serverClient, msg wireMessage) {
	select {
	case c.send <- msg:
	default:
	}
}

func decodeMessage(line string) (wireMessage, error) {
	var msg wireMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return wireMessage{}, err
	}
	return msg, nil
}

func writeMessage(conn net.Conn, msg wireMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(raw, '\n'))
	return err
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func isWildcardAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "0.0.0.0" || host == "::"
}

func dialAddrForHost(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return listenAddr
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func randomID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func runWithReconnect(runOnce func() int) int {
	backoff := 500 * time.Millisecond
	for attempts := 0; attempts < 7; attempts++ {
		code := runOnce()
		if code != 2 {
			return code
		}
		fmt.Printf("Estado: reconectando (intento %d)\n", attempts+1)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 6*time.Second {
			backoff = 6 * time.Second
		}
	}
	fmt.Println("Estado: degradado (sin reconexion)")
	return 1
}

func renderWireMessage(msg wireMessage, myNick string) {
	t := time.Unix(msg.At, 0).Format("15:04:05")
	fromLabel := displayNick(msg.From, myNick)
	toLabel := displayNick(msg.To, myNick)
	switch msg.Type {
	case msgTypeWelcome:
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
		if strings.TrimSpace(msg.Assigned) != "" {
			fmt.Printf("[%s] %s %s\n", t, colorizeSystem("Tu nombre:"), msg.Assigned)
		}
		if msg.Code != "" {
			fmt.Printf("[%s] %s %s\n", t, colorizeSystem("Codigo:"), msg.Code)
		}
		if len(msg.Peers) > 0 {
			fmt.Printf("[%s] %s %s\n", t, colorizeSystem("Peers:"), strings.Join(msg.Peers, ", "))
		}
	case msgTypeSystem:
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
	case msgTypeError:
		fmt.Printf("[%s] %s\n", t, colorizeError(msg.Text))
	case msgTypePeers:
		fmt.Printf("[%s] %s %s\n", t, colorizeSystem("Peers:"), strings.Join(msg.Peers, ", "))
	case msgTypeDiag:
		fmt.Printf("[%s] %s estado=%s rtt=%dms relay=%t nat=%s\n", t, colorizeSystem("Diag:"), msg.State, msg.RTTMs, msg.RelayUsed, msg.NATEst)
	case msgTypeChat:
		fmt.Printf("[%s] %s: %s\n", t, fromLabel, msg.Text)
	case msgTypePrivate:
		if myNick != "" && msg.To != myNick && msg.From != myNick {
			return
		}
		fmt.Printf("[%s] [privado] %s -> %s: %s\n", t, fromLabel, toLabel, msg.Text)
	case msgTypeReaction:
		fmt.Printf("[%s] %s reacciono %s\n", t, fromLabel, msg.Text)
	case msgTypeFile:
		path, size, err := saveIncomingFile(msg)
		if err != nil {
			fmt.Printf("[%s] %s envio archivo %s (no se pudo guardar: %v)\n", t, fromLabel, msg.FileName, err)
		} else {
			fmt.Printf("[%s] %s envio archivo %s (%d bytes) guardado en: %s\n", t, fromLabel, msg.FileName, size, path)
		}
	}
}

func displayNick(nick, myNick string) string {
	n := strings.TrimSpace(nick)
	if n == "" {
		return colorizeName(nick)
	}
	label := n
	if n == strings.TrimSpace(myNick) {
		label = n + " (tu)"
	}
	return colorizeName(label)
}

func shouldSkipOwnEcho(msg wireMessage, myNick string) bool {
	mine := strings.TrimSpace(myNick)
	from := strings.TrimSpace(msg.From)
	if mine == "" || from == "" || from != mine {
		return false
	}
	switch msg.Type {
	case msgTypeChat, msgTypePrivate, msgTypeReaction, msgTypeFile:
		return true
	default:
		return false
	}
}

func clearConsole() {
	fmt.Print("\033[2J\033[H")
}

func parseAtMention(line string) (to, text string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "@") {
		return "", "", false
	}
	rest := strings.TrimSpace(line[1:])
	idx := strings.IndexByte(rest, ' ')
	if idx < 0 {
		return "", "", false
	}
	user := strings.TrimSpace(rest[:idx])
	msg := strings.TrimSpace(rest[idx+1:])
	if user == "" || msg == "" {
		return "", "", false
	}
	return user, msg, true
}

func parsePrivateCommand(line string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "/msg ")), " ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func isReaction(v string) bool {
	s := strings.TrimSpace(v)
	if s == "+1" || s == "-1" || s == "ok" {
		return true
	}
	return strings.HasPrefix(s, ":") && strings.HasSuffix(s, ":") && len(s) > 2
}

func buildFileMessage(path string) (wireMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return wireMessage{}, err
	}
	if len(raw) > 1_500_000 {
		return wireMessage{}, fmt.Errorf("archivo excede 1.5MB")
	}
	name := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		name = path[idx+1:]
	}
	return wireMessage{
		Type:        msgTypeFile,
		FileName:    name,
		FilePayload: base64.StdEncoding.EncodeToString(raw),
		At:          time.Now().Unix(),
	}, nil
}

func saveIncomingFile(msg wireMessage) (string, int, error) {
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return "", 0, err
	}
	if len(raw) > 1_500_000 {
		return "", 0, fmt.Errorf("archivo recibido excede limite")
	}
	name := sanitizeFileName(msg.FileName)
	if name == "" {
		name = "archivo.bin"
	}
	dir, err := downloadDir()
	if err != nil {
		return "", 0, err
	}
	filePath := filepath.Join(dir, fmt.Sprintf("brinco-%d-%s", time.Now().Unix(), name))
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		return "", 0, err
	}
	return filePath, len(raw), nil
}

func downloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		downloads := filepath.Join(home, "Downloads")
		if st, serr := os.Stat(downloads); serr == nil && st.IsDir() {
			return downloads, nil
		}
	}
	return os.Getwd()
}

func sanitizeFileName(name string) string {
	n := filepath.Base(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, ":", "_")
	n = strings.ReplaceAll(n, "\\", "_")
	n = strings.ReplaceAll(n, "/", "_")
	if n == "." || n == "" {
		return ""
	}
	return n
}

func colorizeName(name string) string {
	if !supportsColor() {
		return name
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return name
	}
	nickColorMu.Lock()
	code, ok := nickColorByName[key]
	if !ok {
		code = nickColorCodes[nextNickColorIdx%len(nickColorCodes)]
		nextNickColorIdx++
		nickColorByName[key] = code
	}
	nickColorMu.Unlock()
	return "\x1b[" + code + "m" + name + "\x1b[0m"
}

func colorizeSystem(text string) string {
	if !supportsColor() {
		return "* " + text
	}
	return "\x1b[96m* " + text + "\x1b[0m"
}

func colorizeError(text string) string {
	if !supportsColor() {
		return "! " + text
	}
	return "\x1b[91m! " + text + "\x1b[0m"
}

func supportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "dumb" {
		return false
	}
	return true
}
