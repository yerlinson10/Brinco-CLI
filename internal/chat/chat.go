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

	"brinco-cli/internal/clipboard"
	"brinco-cli/internal/logx"
	"brinco-cli/internal/notify"
	"brinco-cli/internal/roomcode"
	"brinco-cli/internal/roomproto"
)

const (
	msgTypeCreate     = "create"
	msgTypeJoin       = "join"
	msgTypeWelcome    = "welcome"
	msgTypeChat       = "chat"
	msgTypeSystem     = "system"
	msgTypeError      = "error"
	msgTypePeersReq   = "peers_req"
	msgTypePeers      = "peers"
	msgTypeQuit       = "quit"
	msgTypePrivate    = "private"
	msgTypeReaction   = "reaction"
	msgTypeFile       = "file"
	msgTypeDiagReq    = "diag_req"
	msgTypeDiag       = "diag"
	msgTypeModCmd     = "mod_cmd"
	msgTypeHostUpdate = "host_update"
	msgTypeFileChunk  = "file_chunk"

	roomModeDirect     = "direct"
	roomModeRelay      = "relay"
	roomModeGuaranteed = "guaranteed"

	lastRoomCodeFile = "brinco-last-room-code.txt"

	// Dorado reservado al host (no forma parte de la paleta rotativa).
	hostNickANSI = "38;5;220"
)

var (
	nickColorCodes = []string{
		"38;5;33", "38;5;39", "38;5;45", "38;5;51", "38;5;50", "38;5;49",
		"38;5;48", "38;5;47", "38;5;46", "38;5;82", "38;5;118", "38;5;154",
		"38;5;190", "38;5;226", "38;5;214", "38;5;208", "38;5;202",
		"38;5;196", "38;5;197", "38;5;198", "38;5;199", "38;5;200", "38;5;201",
		"38;5;129", "38;5;135", "38;5;141", "38;5;147", "38;5;153", "38;5;159",
		"38;5;123", "38;5;87", "38;5;81", "38;5;75", "38;5;69", "38;5;63",
		"38;5;57", "38;5;93", "38;5;99", "38;5;105", "38;5;111", "38;5;117",
	}
	nickColorMu      sync.Mutex
	nickColorByName  = map[string]string{}
	nextNickColorIdx int
	maxFileBytes     int64 = 10 * 1024 * 1024
	incomingFileMu         sync.Mutex
	incomingFiles          = map[string]*incomingFileTransfer{}
)

type roomCodePayload struct {
	Mode   string   `msgpack:"m,omitempty"`
	Addr   string   `msgpack:"a,omitempty"`
	Relay  string   `msgpack:"x,omitempty"`
	Relays []string `msgpack:"y,omitempty"`
	Room   string   `msgpack:"r"`
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
	TransferID  string   `json:"transferId,omitempty"`
	ChunkIndex  int      `json:"chunkIndex,omitempty"`
	ChunkCount  int      `json:"chunkCount,omitempty"`
	ChunkSize   int      `json:"chunkSize,omitempty"`
	TotalSize   int64    `json:"totalSize,omitempty"`
	Checksum    string   `json:"checksum,omitempty"`
	State       string   `json:"state,omitempty"`
	RTTMs       int64    `json:"rttMs,omitempty"`
	RelayUsed   bool     `json:"relayUsed,omitempty"`
	NATEst      string   `json:"natEst,omitempty"`
	// Assigned es el nombre final en sala (tras uniqueName); solo en welcome.
	Assigned string `json:"assigned,omitempty"`
	// Host es el nick del moderador de sala (mismo valor para todos los clientes).
	Host string `json:"host,omitempty"`
	At   int64  `json:"at"`
}

type incomingFileTransfer struct {
	name        string
	checksum    string
	totalSize   int64
	chunks      [][]byte
	got         []bool
	received    int
	lastPercent int
	cleanup     *time.Timer
	ttlBump     uint64
}

func SetMaxFileBytes(limit int64) {
	if limit > 0 {
		maxFileBytes = limit
	}
}

// roomHostTracker guarda el nick del host anunciado por el servidor (welcome / host_update).
type roomHostTracker struct {
	mu   sync.Mutex
	name string
}

func (t *roomHostTracker) set(s string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.name = strings.TrimSpace(s)
	t.mu.Unlock()
}

func (t *roomHostTracker) get() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

type serverClient struct {
	name     string
	conn     net.Conn
	send     chan wireMessage
	last     time.Time
	tokens   float64
	isHost   bool
	joinedAt time.Time
}

type inputRateLimiter struct {
	tokens       float64
	max          float64
	refillPerSec float64
	last         time.Time
}

type roomServer struct {
	roomID       string
	passwordHash string
	code         string

	ln net.Listener

	mu      sync.Mutex
	clients map[*serverClient]struct{}
	muted   map[string]bool
	banned  map[string]bool
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
	clipboard.AnnounceRoomCode(code)
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
	relays := payload.relayCandidates()
	if len(relays) > 0 {
		best, results := SelectBestTCPRelay(relays, relayProbeTimeout)
		printRelayProbeResults(results)
		if best != "" {
			addr = best
		}
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
	encoded, err := roomcode.Encode(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(roomproto.ProtocolDirect, encoded), nil
}

func BuildRelayRoomCode(relayAddr, room string) (string, error) {
	relays := SplitRelayList(relayAddr)
	payload := roomCodePayload{Mode: roomModeRelay, Relays: relays, Room: room}
	encoded, err := roomcode.Encode(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(roomproto.ProtocolRelay, encoded), nil
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
	var rc roomCodePayload
	if err := roomcode.Decode(encoded, &rc); err != nil {
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
		if len(rc.relayCandidates()) == 0 {
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
	if relays := payload.relayCandidates(); len(relays) > 0 {
		return relays[0], payload.Room, nil
	}
	return payload.Addr, payload.Room, nil
}

func (p roomCodePayload) relayCandidates() []string {
	out := append([]string{}, p.Relays...)
	if strings.TrimSpace(p.Relay) != "" {
		out = append([]string{p.Relay}, out...)
	}
	return uniqueStrings(out)
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
		muted:        make(map[string]bool),
		banned:       make(map[string]bool),
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
			if acceptShouldStop(err) {
				return
			}
			logx.Warn("chat accept", "err", err)
			time.Sleep(acceptErrBackoff)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *roomServer) handleConn(conn net.Conn) {
	defer conn.Close()

	writerDone := make(chan struct{})
	client := &serverClient{conn: conn, send: make(chan wireMessage, clientSendChanCap)}
	joined := false
	defer func() {
		if !joined {
			close(client.send)
			<-writerDone
			return
		}
		s.removeClient(client)
		s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s salio", client.name), At: time.Now().Unix()}, nil)
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
		logx.Debug("chat handshake deadline", "err", err)
	}
	if !scanner.Scan() {
		_ = conn.SetReadDeadline(time.Time{})
		if err := scanner.Err(); err != nil {
			logx.Debug("chat handshake scan", "err", err)
		}
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	join, err := decodeMessage(scanner.Text())
	if err != nil || join.Type != msgTypeJoin {
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "handshake invalido", At: time.Now().Unix()}, "handshake")
		return
	}
	if join.Room != s.roomID {
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "sala inexistente", At: time.Now().Unix()}, "handshake")
		return
	}
	if hashPassword(join.Text) != s.passwordHash {
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "password incorrecta", At: time.Now().Unix()}, "handshake")
		return
	}

	name := strings.TrimSpace(join.From)
	if name == "" {
		name = "anon"
	}
	if s.isBannedName(name) {
		enqueueClientWire(client, wireMessage{Type: msgTypeError, Text: "estas baneado de esta sala", At: time.Now().Unix()}, "handshake")
		return
	}
	client.name = s.uniqueName(name)
	s.addClient(client)
	joined = true

	enqueueClientWire(client, wireMessage{
		Type:     msgTypeWelcome,
		Text:     "Conectado a la sala",
		Code:     s.code,
		Peers:    s.peerNames(),
		Assigned: client.name,
		Host:     s.currentHostName(),
		At:       time.Now().Unix(),
	}, "welcome")
	s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s se unio", client.name), At: time.Now().Unix()}, nil)

	decodeFails := 0
	for scanner.Scan() {
		msg, err := decodeMessage(scanner.Text())
		if err != nil {
			decodeFails++
			if decodeFails >= maxDecodeFailures {
				logx.Warn("chat decode failures closing", "remote", conn.RemoteAddr().String())
				return
			}
			continue
		}
		decodeFails = 0
		s.handleClientMessage(client, msg)
		if msg.Type == msgTypeQuit {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		logx.Debug("chat scanner exit", "err", err)
	}
}

func (s *roomServer) handleClientMessage(c *serverClient, msg wireMessage) {
	if !c.allowMessage() {
		enqueueClientWire(c, wireMessage{Type: msgTypeError, Text: "rate limit excedido", At: time.Now().Unix()}, "rate_limit")
		return
	}
	if msg.Type != msgTypePeersReq && msg.Type != msgTypeDiagReq && msg.Type != msgTypeQuit && msg.Type != msgTypeModCmd {
		if s.isMuted(c.name) {
			enqueueClientWire(c, wireMessage{Type: msgTypeError, Text: "estas silenciado por el host", At: time.Now().Unix()}, "muted")
			return
		}
	}
	switch msg.Type {
	case msgTypeChat:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return
		}
		s.broadcast(wireMessage{Type: msgTypeChat, From: c.name, Text: text, At: time.Now().Unix()}, nil)
	case msgTypePeersReq:
		enqueueClientWire(c, wireMessage{Type: msgTypePeers, Peers: s.peerNames(), At: time.Now().Unix()}, "peers")
	case msgTypeDiagReq:
		enqueueClientWire(c, wireMessage{Type: msgTypeDiag, State: "connected", RTTMs: 0, RelayUsed: false, NATEst: "desconocido", At: time.Now().Unix()}, "diag")
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
		if strings.TrimSpace(msg.FileName) == "" || int64(len(msg.FilePayload)) > maxFileBytes*2 {
			return
		}
		s.broadcast(wireMessage{Type: msgTypeFile, From: c.name, FileName: msg.FileName, FilePayload: msg.FilePayload, At: time.Now().Unix()}, nil)
	case msgTypeFileChunk:
		if !validFileChunk(msg) {
			return
		}
		msg.From = c.name
		msg.At = time.Now().Unix()
		s.broadcast(msg, nil)
	case msgTypeQuit:
		return
	case msgTypeModCmd:
		s.handleModerationCommand(c, msg.Text)
	}
}

// allowMessage solo debe llamarse desde la goroutine de handleConn de este
// cliente (una lectura por conexión); no es seguro en concurrencia cruzada.
func (c *serverClient) allowMessage() bool {
	const maxTok = 5.0
	const refillPerSec = 2.0
	now := time.Now()
	if c.last.IsZero() {
		c.last = now
		c.tokens = maxTok
	}
	c.tokens += now.Sub(c.last).Seconds() * refillPerSec
	if c.tokens > maxTok {
		c.tokens = maxTok
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
	if len(s.clients) == 0 {
		c.isHost = true
	}
	c.joinedAt = time.Now()
	s.clients[c] = struct{}{}
}

func (s *roomServer) removeClient(c *serverClient) {
	var newHost string
	s.mu.Lock()
	wasHost := c.isHost
	delete(s.clients, c)
	if wasHost {
		successor := pickSuccessorHost(s.clients)
		if successor != nil {
			successor.isHost = true
			newHost = successor.name
			enqueueClientWire(successor, wireMessage{Type: msgTypeSystem, Text: "Ahora eres host de la sala", At: time.Now().Unix()}, "host_transfer")
		}
	}
	s.mu.Unlock()
	if newHost != "" {
		s.broadcast(wireMessage{Type: msgTypeHostUpdate, Host: newHost, At: time.Now().Unix()}, nil)
	}
}

func (s *roomServer) broadcast(msg wireMessage, except *serverClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c == except {
			continue
		}
		enqueueClientWire(c, msg, "broadcast")
	}
}

func (s *roomServer) sendPrivate(from *serverClient, toName, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	to, amb := findClientInSet(s.clients, toName)
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

func (s *roomServer) isMuted(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muted[normalizeNickKey(name)]
}

func (s *roomServer) isBannedName(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.banned[normalizeNickKey(name)]
}

func (s *roomServer) findClientByName(name string) (match *serverClient, ambiguous bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return findClientInSet(s.clients, name)
}

func (s *roomServer) handleModerationCommand(sender *serverClient, line string) {
	if !sender.isHost {
		enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "solo el host puede moderar", At: time.Now().Unix()}, "mod")
		return
	}
	cmd, user, ok := parseModerationCommand(line)
	if !ok {
		enqueueClientWire(sender, wireMessage{Type: msgTypeError, Text: "uso: /kick|/mute|/unmute|/ban <usuario>", At: time.Now().Unix()}, "mod")
		return
	}
	target, amb := s.findClientByName(user)
	switch cmd {
	case "/mute":
		s.mu.Lock()
		s.muted[normalizeNickKey(user)] = true
		s.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeSystem, Text: "Has sido silenciado por el host", At: time.Now().Unix()}, "mod")
		}
		s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s fue silenciado por el host", user), At: time.Now().Unix()}, nil)
	case "/unmute":
		s.mu.Lock()
		delete(s.muted, normalizeNickKey(user))
		s.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeSystem, Text: "Ya no estas silenciado", At: time.Now().Unix()}, "mod")
		}
		s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s ya puede escribir", user), At: time.Now().Unix()}, nil)
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
		s.mu.Lock()
		s.banned[normalizeNickKey(user)] = true
		delete(s.muted, normalizeNickKey(user))
		s.mu.Unlock()
		if target != nil {
			enqueueClientWire(target, wireMessage{Type: msgTypeError, Text: "baneado por el host", At: time.Now().Unix()}, "ban")
			_ = target.conn.Close()
		}
		s.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s fue baneado por el host", user), At: time.Now().Unix()}, nil)
	}
}

func parseModerationCommand(line string) (cmd, user string, ok bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return "", "", false
	}
	c := strings.ToLower(parts[0])
	if c != "/kick" && c != "/mute" && c != "/unmute" && c != "/ban" {
		return "", "", false
	}
	return c, strings.TrimSpace(parts[1]), true
}

func normalizeNickKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *roomServer) currentHostName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c.isHost {
			return c.name
		}
	}
	return ""
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
	return allocUniqueNick(base, func(n string) bool { return used[n] })
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
					logx.Warn("chat client decode failures closing", "remote", conn.RemoteAddr().String())
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
			logx.Debug("chat client stdin", "err", err)
		}
		close(stdinLines)
	}()

	localLimiter := &inputRateLimiter{tokens: 3, max: 3, refillPerSec: 0.55, last: time.Now()}
	history := make([]string, 0, 50)
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
					fmt.Println("Comandos: /code /peers /diag /clear /history !! /kick|/mute|/unmute|/ban @usuario mensaje | /msg u t | /send archivo /quit /help")
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

func sendDirect(c *serverClient, msg wireMessage) {
	enqueueClientWire(c, msg, "direct")
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

type RelayProbeResult struct {
	Addr    string
	RTT     time.Duration
	OK      bool
	Message string
}

func SplitRelayList(value string) []string {
	return uniqueStrings(strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	}))
}

func SelectBestTCPRelay(relays []string, timeout time.Duration) (string, []RelayProbeResult) {
	results := ProbeTCPRelays(relays, timeout)
	best := ""
	var bestRTT time.Duration
	for _, result := range results {
		if !result.OK {
			continue
		}
		if best == "" || result.RTT < bestRTT {
			best = result.Addr
			bestRTT = result.RTT
		}
	}
	if best == "" && len(relays) > 0 {
		best = relays[0]
	}
	return best, results
}

func ProbeTCPRelays(relays []string, timeout time.Duration) []RelayProbeResult {
	if timeout <= 0 {
		timeout = time.Second
	}
	relays = uniqueStrings(relays)
	results := make([]RelayProbeResult, 0, len(relays))
	for _, relay := range relays {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", relay, timeout)
		if err != nil {
			results = append(results, RelayProbeResult{Addr: relay, OK: false, Message: err.Error()})
			continue
		}
		_ = conn.Close()
		results = append(results, RelayProbeResult{Addr: relay, OK: true, RTT: time.Since(start), Message: "ok"})
	}
	return results
}

func printRelayProbeResults(results []RelayProbeResult) {
	if len(results) <= 1 {
		return
	}
	for _, result := range results {
		if result.OK {
			fmt.Printf("relay %s disponible (%s)\n", result.Addr, result.RTT.Round(time.Millisecond))
		} else {
			fmt.Printf("relay %s no disponible (%s)\n", result.Addr, result.Message)
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func formatBytes(n int64) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func renderWireMessage(msg wireMessage, myNick string, ht *roomHostTracker) {
	if ht != nil && msg.Type == msgTypeWelcome && strings.TrimSpace(msg.Host) != "" {
		ht.set(msg.Host)
	}
	if msg.Type == msgTypeHostUpdate && strings.TrimSpace(msg.Host) != "" {
		if ht != nil {
			ht.set(msg.Host)
		}
		t := time.Unix(msg.At, 0).Format("15:04:05")
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s\n", t, colorizeSystem("Nuevo host de la sala: "+strings.TrimSpace(msg.Host)))
		return
	}

	t := time.Unix(msg.At, 0).Format("15:04:05")
	hostVal := ""
	if ht != nil {
		hostVal = ht.get()
	}
	fromLabel := displayNick(msg.From, myNick, hostVal)
	toLabel := displayNick(msg.To, myNick, hostVal)
	switch msg.Type {
	case msgTypeWelcome:
		triggerNotification(msg, myNick)
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
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
	case msgTypeError:
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s\n", t, colorizeError(msg.Text))
	case msgTypePeers:
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s %s\n", t, colorizeSystem("Peers:"), strings.Join(msg.Peers, ", "))
	case msgTypeDiag:
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s estado=%s rtt=%dms relay=%t nat=%s\n", t, colorizeSystem("Diag:"), msg.State, msg.RTTMs, msg.RelayUsed, msg.NATEst)
	case msgTypeChat:
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s: %s\n", t, fromLabel, msg.Text)
	case msgTypePrivate:
		if myNick != "" && msg.To != myNick && msg.From != myNick {
			return
		}
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] [privado] %s -> %s: %s\n", t, fromLabel, toLabel, msg.Text)
	case msgTypeReaction:
		triggerNotification(msg, myNick)
		fmt.Printf("[%s] %s reacciono %s\n", t, fromLabel, msg.Text)
	case msgTypeFile:
		path, size, err := saveIncomingFile(msg)
		triggerNotification(msg, myNick)
		if err != nil {
			fmt.Printf("[%s] %s envio archivo %s (no se pudo guardar: %v)\n", t, fromLabel, msg.FileName, err)
		} else {
			fmt.Printf("[%s] %s envio archivo %s (%d bytes) guardado en: %s\n", t, fromLabel, msg.FileName, size, path)
		}
	case msgTypeFileChunk:
		result := saveIncomingFileChunk(msg)
		if result.Err != nil {
			fmt.Printf("[%s] %s envio archivo %s (error: %v)\n", t, fromLabel, msg.FileName, result.Err)
			return
		}
		if result.Done {
			triggerNotification(msg, myNick)
			fmt.Printf("[%s] %s envio archivo %s (%d bytes, sha256=%s) guardado en: %s\n", t, fromLabel, msg.FileName, result.Size, msg.Checksum, result.Path)
		} else if result.Percent > 0 {
			fmt.Printf("[%s] recibiendo %s de %s: %d%%\n", t, msg.FileName, fromLabel, result.Percent)
		}
	}
}

func displayNick(nick, myNick, roomHost string) string {
	n := strings.TrimSpace(nick)
	if n == "" {
		return colorizeName(nick)
	}
	if n == strings.TrimSpace(myNick) {
		return colorizeName(n) + " (tu)"
	}
	if rh := strings.TrimSpace(roomHost); rh != "" && strings.EqualFold(n, rh) {
		return colorizeFixed(n, hostNickANSI)
	}
	return colorizeName(n)
}

func nickBaseForPalette(displayed string) string {
	s := strings.TrimSpace(displayed)
	ls := strings.ToLower(s)
	suf := " (tu)"
	if strings.HasSuffix(ls, suf) {
		return strings.TrimSpace(s[:len(s)-len(suf)])
	}
	return s
}

func nickColorKey(name string) string {
	return strings.ToLower(nickBaseForPalette(name))
}

func colorizeFixed(name, code string) string {
	if !supportsColor() {
		return name
	}
	return "\x1b[" + code + "m" + name + "\x1b[0m"
}

func shouldSkipOwnEcho(msg wireMessage, myNick string) bool {
	mine := strings.TrimSpace(myNick)
	from := strings.TrimSpace(msg.From)
	if mine == "" || from == "" || from != mine {
		return false
	}
	switch msg.Type {
	case msgTypeChat:
		return true
	default:
		return false
	}
}

func clearConsole() {
	fmt.Print("\033[2J\033[H")
}

func triggerNotification(msg wireMessage, myNick string) {
	notify.Trigger(notify.Event{Type: msg.Type, From: msg.From, To: msg.To, Text: msg.Text, MyNick: myNick})
}

func printRateLimitLocal() {
	msg := "Rate limit: espera unos segundos antes de seguir enviando."
	if !supportsColor() {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	fmt.Fprintf(os.Stderr, "\x1b[91m%s\x1b[0m\n", msg)
}

func (l *inputRateLimiter) allow() bool {
	now := time.Now()
	if l.last.IsZero() {
		l.last = now
	}
	l.tokens += now.Sub(l.last).Seconds() * l.refillPerSec
	if l.tokens > l.max {
		l.tokens = l.max
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

func appendHistory(history []string, line string, max int) []string {
	if strings.TrimSpace(line) == "" {
		return history
	}
	history = append(history, line)
	if len(history) > max {
		history = history[len(history)-max:]
	}
	return history
}

func printInputHistory(history []string) {
	if len(history) == 0 {
		fmt.Println("Historial vacio")
		return
	}
	start := 0
	if len(history) > 20 {
		start = len(history) - 20
	}
	for i := start; i < len(history); i++ {
		fmt.Printf("%2d) %s\n", i-start+1, history[i])
	}
}

func isModerationCommandLine(line string) bool {
	cmd, _, ok := parseModerationCommand(line)
	return ok && cmd != ""
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
	if int64(len(raw)) > maxFileBytes {
		return wireMessage{}, fmt.Errorf("archivo excede limite de %s", formatBytes(maxFileBytes))
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

func sendFileChunks(conn net.Conn, path string) error {
	chunks, total, checksum, name, err := buildFileChunks(path)
	if err != nil {
		return err
	}
	for i, msg := range chunks {
		if err := writeMessage(conn, msg); err != nil {
			return err
		}
		percent := ((i + 1) * 100) / len(chunks)
		fmt.Printf("enviando %s: %d%%\n", name, percent)
	}
	fmt.Printf("archivo enviado: %s (%s, sha256=%s)\n", name, formatBytes(total), checksum)
	return nil
}

func buildFileChunks(path string) ([]wireMessage, int64, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, "", "", err
	}
	total := int64(len(raw))
	if total > maxFileBytes {
		return nil, 0, "", "", fmt.Errorf("archivo excede limite de %s", formatBytes(maxFileBytes))
	}
	name := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		name = path[idx+1:]
	}
	const chunkSize = 64 * 1024
	sum := sha256.Sum256(raw)
	checksum := hex.EncodeToString(sum[:])
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, 0, "", "", err
	}
	transferID := hex.EncodeToString(nonce[:]) + checksum[:16]
	chunkCount := int((total + chunkSize - 1) / chunkSize)
	if chunkCount == 0 {
		chunkCount = 1
	}
	chunks := make([]wireMessage, 0, chunkCount)
	for i := 0; i < chunkCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(raw) {
			end = len(raw)
		}
		payload := ""
		if start < len(raw) {
			payload = base64.StdEncoding.EncodeToString(raw[start:end])
		}
		chunks = append(chunks, wireMessage{
			Type:        msgTypeFileChunk,
			FileName:    name,
			FilePayload: payload,
			TransferID:  transferID,
			ChunkIndex:  i,
			ChunkCount:  chunkCount,
			ChunkSize:   chunkSize,
			TotalSize:   total,
			Checksum:    checksum,
			At:          time.Now().Unix(),
		})
	}
	return chunks, total, checksum, name, nil
}

type fileChunkResult struct {
	Path    string
	Size    int
	Percent int
	Done    bool
	Err     error
}

func rescheduleIncomingFileTTL(transferID string, tr *incomingFileTransfer) {
	tr.ttlBump++
	b := tr.ttlBump
	ptr := tr
	if tr.cleanup != nil {
		if !tr.cleanup.Stop() {
			select {
			case <-tr.cleanup.C:
			default:
			}
		}
	}
	tr.cleanup = time.AfterFunc(incomingFileTransferTTL, func() {
		incomingFileMu.Lock()
		defer incomingFileMu.Unlock()
		cur, ok := incomingFiles[transferID]
		if !ok || cur != ptr || cur.ttlBump != b {
			return
		}
		delete(incomingFiles, transferID)
	})
}

func stopIncomingFileTTL(tr *incomingFileTransfer) {
	if tr == nil || tr.cleanup == nil {
		return
	}
	if !tr.cleanup.Stop() {
		select {
		case <-tr.cleanup.C:
		default:
		}
	}
	tr.cleanup = nil
}

func saveIncomingFileChunk(msg wireMessage) fileChunkResult {
	if !validFileChunk(msg) {
		return fileChunkResult{Err: fmt.Errorf("chunk invalido")}
	}
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return fileChunkResult{Err: err}
	}
	incomingFileMu.Lock()
	defer incomingFileMu.Unlock()

	tr := incomingFiles[msg.TransferID]
	if tr == nil {
		tr = &incomingFileTransfer{
			name:      msg.FileName,
			checksum:  msg.Checksum,
			totalSize: msg.TotalSize,
			chunks:    make([][]byte, msg.ChunkCount),
			got:       make([]bool, msg.ChunkCount),
		}
		incomingFiles[msg.TransferID] = tr
	}
	if msg.ChunkIndex >= len(tr.chunks) {
		stopIncomingFileTTL(tr)
		delete(incomingFiles, msg.TransferID)
		return fileChunkResult{Err: fmt.Errorf("chunk fuera de rango")}
	}
	if !tr.got[msg.ChunkIndex] {
		tr.got[msg.ChunkIndex] = true
		tr.chunks[msg.ChunkIndex] = raw
		tr.received++
	}
	rescheduleIncomingFileTTL(msg.TransferID, tr)
	percent := (tr.received * 100) / len(tr.chunks)
	if tr.received < len(tr.chunks) {
		if percent >= tr.lastPercent+10 || percent == 100 {
			tr.lastPercent = percent
			return fileChunkResult{Percent: percent}
		}
		return fileChunkResult{}
	}

	assembled := make([]byte, 0, tr.totalSize)
	for _, chunk := range tr.chunks {
		assembled = append(assembled, chunk...)
	}
	if int64(len(assembled)) != tr.totalSize {
		stopIncomingFileTTL(tr)
		delete(incomingFiles, msg.TransferID)
		return fileChunkResult{Err: fmt.Errorf("tamano recibido invalido")}
	}
	sum := sha256.Sum256(assembled)
	if hex.EncodeToString(sum[:]) != tr.checksum {
		stopIncomingFileTTL(tr)
		delete(incomingFiles, msg.TransferID)
		return fileChunkResult{Err: fmt.Errorf("checksum invalido")}
	}
	path, err := writeIncomingFile(tr.name, assembled)
	if err != nil {
		stopIncomingFileTTL(tr)
		delete(incomingFiles, msg.TransferID)
		return fileChunkResult{Err: err}
	}
	stopIncomingFileTTL(tr)
	delete(incomingFiles, msg.TransferID)
	return fileChunkResult{Path: path, Size: len(assembled), Percent: 100, Done: true}
}

func validFileChunk(msg wireMessage) bool {
	tid := strings.TrimSpace(msg.TransferID)
	return tid != "" && len(tid) <= 96 &&
		strings.TrimSpace(msg.FileName) != "" &&
		msg.ChunkCount > 0 &&
		msg.ChunkIndex >= 0 &&
		msg.ChunkIndex < msg.ChunkCount &&
		msg.TotalSize >= 0 &&
		msg.TotalSize <= maxFileBytes &&
		strings.TrimSpace(msg.Checksum) != "" &&
		int64(len(msg.FilePayload)) <= maxFileBytes*2
}

func saveIncomingFile(msg wireMessage) (string, int, error) {
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return "", 0, err
	}
	if int64(len(raw)) > maxFileBytes {
		return "", 0, fmt.Errorf("archivo recibido excede limite")
	}
	path, err := writeIncomingFile(msg.FileName, raw)
	return path, len(raw), err
}

func writeIncomingFile(name string, raw []byte) (string, error) {
	safeName := sanitizeFileName(name)
	if safeName == "" {
		safeName = "archivo.bin"
	}
	dir, err := downloadDir()
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(dir, fmt.Sprintf("brinco-%d-%s", time.Now().Unix(), safeName))
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		return "", err
	}
	return filePath, nil
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
	key := nickColorKey(name)
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
