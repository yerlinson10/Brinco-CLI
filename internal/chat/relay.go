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
}

func RunCreateUsingRelay(name, relayAddr, password string) int {
	if strings.TrimSpace(relayAddr) == "" {
		fmt.Fprintln(os.Stderr, "Error: --relay es obligatorio en modo relay")
		return 1
	}
	if strings.TrimSpace(password) == "" {
		fmt.Fprintln(os.Stderr, "Error: --password es obligatorio para crear sala")
		return 1
	}
	if strings.TrimSpace(name) == "" {
		name = "host"
	}
	fmt.Printf("Conectando al relay %s...\n", relayAddr)
	return runRelayClient(relayAddr, "", name, password, "", true)
}

func RunRelayServer(listenAddr, publicAddr string) int {
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
			return 1
		}
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
		sendDirect(client, wireMessage{Type: msgTypeWelcome, Text: "Sala relay creada", Code: room.code, Peers: room.peerNames(), At: time.Now().Unix()})
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
		client.name = room.uniqueName(name)
		room.addClient(client)
		sendDirect(client, wireMessage{Type: msgTypeWelcome, Text: "Conectado a la sala relay", Code: room.code, Peers: room.peerNames(), At: time.Now().Unix()})
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
		switch msg.Type {
		case msgTypeChat:
			text := strings.TrimSpace(msg.Text)
			if text == "" {
				continue
			}
			room.broadcast(wireMessage{Type: msgTypeChat, From: client.name, Text: text, At: time.Now().Unix()}, nil)
		case msgTypePeersReq:
			sendDirect(client, wireMessage{Type: msgTypePeers, Peers: room.peerNames(), At: time.Now().Unix()})
		case msgTypeQuit:
			goto cleanup
		}
	}

cleanup:
	room.removeClient(client)
	room.broadcast(wireMessage{Type: msgTypeSystem, Text: fmt.Sprintf("%s salio", client.name), At: time.Now().Unix()}, nil)
	if room.clientCount() == 0 {
		h.deleteRoom(room.id)
	}
	close(client.send)
	<-writerDone
}

func (h *relayHub) createRoom(password string) (*relayRoom, error) {
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("password obligatoria")
	}
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
	r.clients[c] = struct{}{}
}

func (r *relayRoom) removeClient(c *serverClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
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

func runRelayClient(relayAddr, roomID, name, password, roomCode string, create bool) int {
	conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error conectando: %v\n", err)
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

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			msg, err := decodeMessage(scanner.Text())
			if err != nil {
				continue
			}
			if msg.Type == msgTypeWelcome && strings.TrimSpace(msg.Code) != "" {
				select {
				case codeCh <- msg.Code:
				default:
				}
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
		case c := <-codeCh:
			roomCode = c
			_ = SaveLastRoomCode(c)
		default:
		}

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
