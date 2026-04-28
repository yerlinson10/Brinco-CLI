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
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

	roomModeDirect = "direct"
	roomModeRelay  = "relay"

	lastRoomCodeFile = "brinco-last-room-code.txt"
)

type roomCodePayload struct {
	Mode  string `json:"mode,omitempty"`
	Addr  string `json:"addr,omitempty"`
	Relay string `json:"relay,omitempty"`
	Room  string `json:"room"`
}

type wireMessage struct {
	Type  string   `json:"type"`
	Room  string   `json:"room,omitempty"`
	From  string   `json:"from,omitempty"`
	Text  string   `json:"text,omitempty"`
	Peers []string `json:"peers,omitempty"`
	Code  string   `json:"code,omitempty"`
	At    int64    `json:"at"`
}

type serverClient struct {
	name string
	conn net.Conn
	send chan wireMessage
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
	if strings.TrimSpace(password) == "" {
		fmt.Fprintln(os.Stderr, "Error: --password es obligatorio para crear sala")
		return 1
	}
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

	dialAddr := dialAddrForHost(listenAddr)
	return runClient(dialAddr, roomID, name, password, code)
}

func RunJoin(name, code, password string) int {
	if strings.TrimSpace(code) == "" {
		fmt.Fprintln(os.Stderr, "Error: --code es obligatorio")
		return 1
	}
	if strings.TrimSpace(password) == "" {
		fmt.Fprintln(os.Stderr, "Error: --password es obligatorio")
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
	mode := payload.Mode
	if mode == "" {
		mode = roomModeDirect
	}

	fmt.Printf("Conectando a %s...\n", addr)
	if mode == roomModeRelay {
		return runRelayClient(addr, roomID, name, password, code, false)
	}
	return runClient(addr, roomID, name, password, code)
}

func BuildRoomCode(addr, room string) (string, error) {
	payload := roomCodePayload{Mode: roomModeDirect, Addr: addr, Room: room}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func BuildRelayRoomCode(relayAddr, room string) (string, error) {
	payload := roomCodePayload{Mode: roomModeRelay, Relay: relayAddr, Room: room}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ParseRoomCodeDetailed(code string) (roomCodePayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if err != nil {
		return roomCodePayload{}, err
	}

	var payload roomCodePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return roomCodePayload{}, err
	}
	if payload.Room == "" {
		return roomCodePayload{}, errors.New("codigo invalido")
	}
	if strings.TrimSpace(payload.Mode) == "" {
		payload.Mode = roomModeDirect
	}
	if payload.Mode == roomModeRelay {
		if strings.TrimSpace(payload.Relay) == "" {
			return roomCodePayload{}, errors.New("codigo relay invalido")
		}
		return payload, nil
	}
	if strings.TrimSpace(payload.Addr) == "" {
		return roomCodePayload{}, errors.New("codigo directo invalido")
	}
	return payload, nil
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
		Type:  msgTypeWelcome,
		Text:  "Conectado a la sala",
		Code:  s.code,
		Peers: s.peerNames(),
		At:    time.Now().Unix(),
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
	switch msg.Type {
	case msgTypeChat:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		s.broadcast(wireMessage{Type: msgTypeChat, From: c.name, Text: text, At: time.Now().Unix()}, nil)
	case msgTypePeersReq:
		sendDirect(c, wireMessage{Type: msgTypePeers, Peers: s.peerNames(), At: time.Now().Unix()})
	case msgTypeQuit:
		return
	}
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

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			msg, err := decodeMessage(scanner.Text())
			if err != nil {
				continue
			}
			renderMessage(msg)
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	fmt.Println("Escribe mensajes y Enter para enviar")
	fmt.Println("Comandos: /code /peers /quit /help")

	stdin := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-done:
			select {
			case err := <-errCh:
				fmt.Fprintf(os.Stderr, "Conexion cerrada: %v\n", err)
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

		if strings.HasPrefix(line, "/") {
			switch line {
			case "/quit":
				_ = writeMessage(conn, wireMessage{Type: msgTypeQuit, At: time.Now().Unix()})
				return 0
			case "/peers":
				_ = writeMessage(conn, wireMessage{Type: msgTypePeersReq, At: time.Now().Unix()})
			case "/code":
				if strings.TrimSpace(roomCode) != "" {
					fmt.Printf("Codigo de sala: %s\n", roomCode)
				} else {
					fmt.Println("No hay codigo disponible en esta sesion")
				}
			case "/help":
				fmt.Println("Comandos: /code /peers /quit /help")
			default:
				fmt.Println("Comando no reconocido. Usa /help")
			}
			continue
		}

		if err := writeMessage(conn, wireMessage{Type: msgTypeChat, Text: line, At: time.Now().Unix()}); err != nil {
			fmt.Fprintf(os.Stderr, "Error enviando mensaje: %v\n", err)
			return 1
		}
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

func renderMessage(msg wireMessage) {
	t := time.Unix(msg.At, 0).Format("15:04:05")
	switch msg.Type {
	case msgTypeWelcome:
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
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
	case msgTypeChat:
		fmt.Printf("[%s] %s: %s\n", t, colorizeName(msg.From), msg.Text)
	}
}

func colorizeName(name string) string {
	if !supportsColor() {
		return name
	}
	colors := []string{"31", "32", "33", "34", "35", "36", "91", "92", "93", "94", "95", "96"}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := int(h.Sum32()) % len(colors)
	return "\x1b[" + colors[idx] + "m" + name + "\x1b[0m"
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
