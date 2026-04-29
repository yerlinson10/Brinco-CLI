package p2p

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"brinco-cli/internal/logx"
	"brinco-cli/internal/roomproto"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

// Bootstrap nodes de la red IPFS/libp2p - publicos y gratuitos
var defaultBootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
}

type chatMessage struct {
	ID          string `json:"id,omitempty"`
	From        string `json:"from"`
	Text        string `json:"text"`
	Type        string `json:"type"` // chat | system | private | reaction | file
	At          int64  `json:"at"`
	To          string `json:"to,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	FilePayload string `json:"filePayload,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type encryptedMessage struct {
	Nonce  string `json:"nonce"`
	Cipher string `json:"cipher"`
}

type roomCodePayload struct {
	Topic string   `json:"topic"`
	Relay string   `json:"relay,omitempty"`
	Peers []string `json:"peers,omitempty"`
}

// Node es el nodo libp2p del usuario
type Node struct {
	host      host.Host
	ps        *pubsub.PubSub
	topic     *pubsub.Topic
	sub       *pubsub.Subscription
	name      string
	ctx       context.Context
	cancel    context.CancelFunc
	roomPeers []string

	seenMu   sync.Mutex
	seenMsgs map[string]time.Time

	statusMu         sync.RWMutex
	connectivity     string
	lastConnectivity time.Time
	peerFingerprints map[string]string
	rate             map[string]*peerRate
	roomCode         string
	roomSecret       [32]byte
}

var (
	nickColorCodesP2P   = []string{
		"38;5;220", "38;5;226", "38;5;33", "38;5;39", "38;5;45", "38;5;51", "38;5;50", "38;5;49",
		"38;5;48", "38;5;47", "38;5;46", "38;5;82", "38;5;118", "38;5;154",
		"38;5;190", "38;5;214", "38;5;208", "38;5;202",
		"38;5;196", "38;5;197", "38;5;198", "38;5;199", "38;5;200", "38;5;201",
		"38;5;129", "38;5;135", "38;5;141", "38;5;147", "38;5;153", "38;5;159",
		"38;5;123", "38;5;87", "38;5;81", "38;5;75", "38;5;69", "38;5;63",
		"38;5;57", "38;5;93", "38;5;99", "38;5;105", "38;5;111", "38;5;117",
	}
	nickColorMuP2P      sync.Mutex
	nickColorByNameP2P  = map[string]string{}
	nextNickColorIdxP2P int
)

// NewNodeGuaranteed crea un nodo libp2p que usa el DHT de la red IPFS para
// descubrir relays circuit v2 automaticamente. Fuerza reachability privada para
// que el auto-relay siempre busque y reserve slots en relays disponibles.
func NewNodeGuaranteed(ctx context.Context, extraRelayAddrs []string) (*Node, error) {
	retx, cancel := context.WithCancel(ctx)
	logx.Debug("p2p guaranteed node init (DHT + AutoRelay)")

	// Variable que se asigna despues de crear el host.
	var kdht *dht.IpfsDHT
	var h host.Host

	// Peer source para AutoRelay: busca peers del routing table del DHT.
	peerSource := func(pctx context.Context, numPeers int) <-chan peer.AddrInfo {
		ch := make(chan peer.AddrInfo, numPeers)
		go func() {
			defer close(ch)
			if kdht == nil || h == nil {
				return
			}
			peers := kdht.RoutingTable().ListPeers()
			count := 0
			for _, p := range peers {
				if count >= numPeers {
					break
				}
				addrs := h.Peerstore().Addrs(p)
				if len(addrs) == 0 {
					continue
				}
				select {
				case ch <- peer.AddrInfo{ID: p, Addrs: addrs}:
					count++
				case <-pctx.Done():
					return
				}
			}
		}()
		return ch
	}

	opts := []libp2p.Option{
		libp2p.NoTransports,
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip6/::/tcp/0",
		),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(),
		libp2p.ForceReachabilityPrivate(),
		libp2p.EnableAutoRelayWithPeerSource(peerSource),
		libp2p.EnableHolePunching(),
	}

	var err error
	h, err = libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error creando nodo: %w", err)
	}

	// DHT en modo cliente: descubre peers relay sin servir como router.
	kdht, err = dht.New(retx, h, dht.Mode(dht.ModeClient))
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error creando DHT: %w", err)
	}
	if err := kdht.Bootstrap(retx); err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error bootstrap DHT: %w", err)
	}

	ps, err := pubsub.NewGossipSub(
		retx,
		h,
		pubsub.WithPeerExchange(true),
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error creando pubsub: %w", err)
	}

	n := &Node{host: h, ps: ps, ctx: retx, cancel: cancel, seenMsgs: make(map[string]time.Time), peerFingerprints: make(map[string]string), rate: make(map[string]*peerRate)}
	n.setConnectivity("connected")
	return n, nil
}

// NewNode crea un nodo libp2p con relay circuit habilitado
func NewNode(ctx context.Context, relayAddrs []string) (*Node, error) {
	ctx, cancel := context.WithCancel(ctx)
	logx.Debug("p2p node init", "relays", len(relayAddrs))

	opts := []libp2p.Option{
		// En Windows evitamos QUIC por un panic conocido de quic-go en algunos entornos.
		// Dejamos solo TCP para estabilidad.
		libp2p.NoTransports,
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip6/::/tcp/0",
		),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(),
		libp2p.EnableAutoRelayWithStaticRelays(peerInfosFromAddrs(relayAddrs)),
		libp2p.EnableHolePunching(),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error creando nodo: %w", err)
	}

	ps, err := pubsub.NewGossipSub(
		ctx,
		h,
		pubsub.WithPeerExchange(true),
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error creando pubsub: %w", err)
	}

	n := &Node{host: h, ps: ps, ctx: ctx, cancel: cancel, seenMsgs: make(map[string]time.Time), peerFingerprints: make(map[string]string), rate: make(map[string]*peerRate)}
	n.setConnectivity("connected")
	return n, nil
}

// Bootstrap conecta a la red via nodos publicos de IPFS y descubrimiento mDNS
func (n *Node) Bootstrap(customRelays []string) {
	peers := append(defaultBootstrapPeers, customRelays...)
	var wg sync.WaitGroup
	for _, addrStr := range peers {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			ma, err := multiaddr.NewMultiaddr(a)
			if err != nil {
				return
			}
			pi, err := peer.AddrInfoFromP2pAddr(ma)
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
			defer cancel()
			_ = n.host.Connect(ctx, *pi)
		}(addrStr)
	}
	wg.Wait()

	// mDNS para descubrimiento local (LAN)
	svc := mdns.NewMdnsService(n.host, "brinco-chat", &mdnsNotifee{host: n.host, ctx: n.ctx})
	_ = svc.Start()
}

// WaitForRelay espera hasta obtener al menos una direccion relay (p2p-circuit)
// o hasta que expire el timeout. Devuelve true si se encontro relay.
func (n *Node) WaitForRelay(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, addr := range n.host.Addrs() {
			if strings.Contains(addr.String(), "p2p-circuit") {
				return true
			}
		}
		select {
		case <-n.ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

// JoinTopic suscribe al topic de la sala
func (n *Node) JoinTopic(topic string) error {
	t, err := n.ps.Join(topic)
	if err != nil {
		return err
	}
	sub, err := t.Subscribe()
	if err != nil {
		return err
	}
	n.topic = t
	n.sub = sub
	return nil
}

// Publish envía un mensaje al topic de la sala
func (n *Node) Publish(text string, msgType string) error {
	msg := chatMessage{ID: newMessageID(), From: n.name, Text: text, Type: msgType, At: time.Now().Unix()}
	return n.publishMessage(msg)
}

func (n *Node) publishMessage(msg chatMessage) error {
	rawPlain, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	raw := rawPlain
	if msg.Type != "system" {
		raw, err = n.encryptPayload(rawPlain)
		if err != nil {
			return err
		}
	}
	return n.topic.Publish(n.ctx, raw)
}

// TopicPeerCount devuelve cuantos peers conocidos hay en el topic actual.
func (n *Node) TopicPeerCount() int {
	if n.topic == nil {
		return 0
	}
	return len(n.ps.ListPeers(n.topic.String()))
}

// SetName asigna el nombre del usuario
func (n *Node) SetName(name string) {
	n.name = name
}

func (n *Node) SetRoomPeers(peers []string) {
	n.roomPeers = append([]string(nil), peers...)
}

func (n *Node) SetRoomCode(code string) {
	n.roomCode = strings.TrimSpace(code)
	n.roomSecret = sha256.Sum256([]byte(n.roomCode))
}

// Close cierra el nodo
func (n *Node) Close() {
	n.cancel()
	_ = n.host.Close()
}

// ID devuelve el peer ID del nodo
func (n *Node) ID() string {
	return n.host.ID().String()
}

// BuildRoomCode genera un codigo de sala a partir del topic (y relay opcional)
func BuildRoomCode(topic, relayAddr string, peers []string) (string, error) {
	return BuildRoomCodeWithProtocol(topic, relayAddr, peers, roomproto.ProtocolP2P)
}

// BuildRoomCodeWithProtocol genera un codigo de sala con el prefijo de protocolo indicado.
func BuildRoomCodeWithProtocol(topic, relayAddr string, peers []string, protocol string) (string, error) {
	payload := roomCodePayload{Topic: topic, Relay: relayAddr, Peers: peers}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(protocol, base64.RawURLEncoding.EncodeToString(raw)), nil
}

func ParseRoomCodeDetailed(code string) (roomCodePayload, error) {
	protocol, encoded := roomproto.Unwrap(code)
	if protocol != "" && protocol != roomproto.ProtocolP2P && protocol != roomproto.ProtocolGuaranteed {
		return roomCodePayload{}, fmt.Errorf("codigo no es p2p")
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
	if rc.Topic == "" {
		return roomCodePayload{}, fmt.Errorf("codigo invalido")
	}
	return rc, nil
}

// ParseRoomCode descodifica un codigo de sala
func ParseRoomCode(code string) (topic string, relayAddr string, err error) {
	payload, err := ParseRoomCodeDetailed(code)
	if err != nil {
		return "", "", err
	}
	return payload.Topic, payload.Relay, nil
}

// AdvertisePeerAddrs devuelve multiaddrs p2p del nodo para que otros peers puedan conectar.
func (n *Node) AdvertisePeerAddrs() []string {
	addrs := n.host.Addrs()
	peers := make([]string, 0, len(addrs))
	for _, a := range addrs {
		full := a.Encapsulate(multiaddr.StringCast("/p2p/" + n.host.ID().String()))
		peers = append(peers, full.String())
	}
	return peers
}

// ConnectToPeers intenta conectar a una lista de multiaddrs de peers.
func (n *Node) ConnectToPeers(peerMultiaddrs []string) {
	for _, addr := range peerMultiaddrs {
		ma, err := multiaddr.NewMultiaddr(strings.TrimSpace(addr))
		if err != nil {
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(n.ctx, 8*time.Second)
		_ = n.host.Connect(ctx, *pi)
		cancel()
	}
}

// ConnectToPeersWithRetry intenta varias veces conectar a peers del codigo de sala.
// Esto evita quedar sin malla cuando hay latencia o establecimiento lento de sockets.
func (n *Node) ConnectToPeersWithRetry(peerMultiaddrs []string, attempts int, wait time.Duration) {
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		n.ConnectToPeers(peerMultiaddrs)
		if i < attempts-1 {
			select {
			case <-n.ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

// RandomTopic genera un topic aleatorio para la sala
func RandomTopic() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "brinco-" + base64.RawURLEncoding.EncodeToString(b), nil
}

// RunChat entra al chat interactivo de la sala
func (n *Node) RunChat(roomCode string) int {
	n.SetRoomCode(roomCode)
	go n.reconnectLoop()
	go n.receiveLoop()

	fmt.Println("Conectado al topic de la sala")
	if n.TopicPeerCount() == 0 {
		fmt.Println("Aviso: aun no hay peers enlazados. Esperando conexiones...")
	}
	fmt.Println("Escribe mensajes y Enter para enviar")
	fmt.Println("Comandos: /code /peers /diag /clear @usuario mensaje | /msg u texto | /send archivo /quit /help")

	stdin := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-n.ctx.Done():
			return 0
		default:
		}

		if !stdin.Scan() {
			_ = n.Publish(n.name+" salio", "system")
			return 0
		}
		line := strings.TrimSpace(stdin.Text())
		if line == "" {
			continue
		}

		if isReaction(line) {
			reactionMsg := chatMessage{ID: newMessageID(), From: n.name, Text: line, Type: "reaction", At: time.Now().Unix(), Fingerprint: n.fingerprint()}
			_ = n.publishMessage(reactionMsg)
			renderMessage(reactionMsg, n.name)
			continue
		}

		if to, txt, ok := parseAtMentionP2P(line); ok {
			pmMsg := chatMessage{ID: newMessageID(), From: n.name, To: to, Text: txt, Type: "private", At: time.Now().Unix(), Fingerprint: n.fingerprint()}
			_ = n.publishMessage(pmMsg)
			renderMessage(pmMsg, n.name)
			continue
		}

		if strings.HasPrefix(line, "/") {
			switch {
			case line == "/quit":
				_ = n.Publish(n.name+" salio", "system")
				return 0
			case line == "/code":
				fmt.Printf("Codigo de sala: %s\n", roomCode)
			case line == "/peers":
				fmt.Printf("Peers enlazados al topic: %d\n", n.TopicPeerCount())
			case line == "/diag":
				n.printDiag()
			case line == "/help":
				fmt.Println("Comandos: /code /peers /diag /clear @usuario mensaje | /msg u texto | /send archivo /quit /help")
			case line == "/clear":
				clearConsoleP2P()
			case strings.HasPrefix(line, "/msg "):
				to, text, ok := parsePrivate(line)
				if !ok {
					fmt.Println("Uso: /msg <usuario> <texto>")
					continue
				}
				pmMsg := chatMessage{ID: newMessageID(), From: n.name, To: to, Text: text, Type: "private", At: time.Now().Unix(), Fingerprint: n.fingerprint()}
				if err := n.publishMessage(pmMsg); err != nil {
					fmt.Fprintf(os.Stderr, "Error enviando privado: %v\n", err)
				} else {
					renderMessage(pmMsg, n.name)
				}
			case strings.HasPrefix(line, "/send "):
				if err := n.sendFile(strings.TrimSpace(strings.TrimPrefix(line, "/send "))); err != nil {
					fmt.Fprintf(os.Stderr, "Error enviando archivo: %v\n", err)
				}
			default:
				fmt.Println("Comando no reconocido. Usa /help")
			}
			continue
		}

		if err := n.publishChatReliable(line); err != nil {
			fmt.Fprintf(os.Stderr, "Error enviando: %v\n", err)
		}
	}
}

func (n *Node) publishChatReliable(text string) error {
	msg := chatMessage{ID: newMessageID(), From: n.name, Text: text, Type: "chat", At: time.Now().Unix(), Fingerprint: n.fingerprint()}
	var lastErr error
	for i := 0; i < 80; i++ {
		if n.TopicPeerCount() == 0 && len(n.roomPeers) > 0 {
			n.setConnectivity("reconnecting")
			n.ConnectToPeers(n.roomPeers)
		}
		if err := n.publishMessage(msg); err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		renderMessage(msg, n.name)
		n.setConnectivity("connected")
		return nil
	}
	n.setConnectivity("degraded")
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no hay peers conectados en la sala (sin ruta directa); prueba --relay o --direct con puertos abiertos")
}

func (n *Node) receiveLoop() {
	for {
		msg, err := n.sub.Next(n.ctx)
		if err != nil {
			return
		}
		// Propios: Topic.Publish usa PushLocal con ReceivedFrom=host; el autor protobuf es nuestro peer ID.
		// Los mostramos con eco local en publishChatReliable; aqui evitamos duplicados y confusion con el siguiente mensaje ajeno.
		if msg.GetFrom() == n.host.ID() || msg.ReceivedFrom == n.host.ID() {
			continue
		}
		var cm chatMessage
		if err := json.Unmarshal(msg.Data, &cm); err != nil {
			plain, derr := n.decryptPayload(msg.Data)
			if derr != nil {
				continue
			}
			if err := json.Unmarshal(plain, &cm); err != nil {
				continue
			}
		}
		if cm.From != "" && !n.allowPeer(cm.From) {
			continue
		}
		if cm.Fingerprint != "" && !n.trackFingerprint(cm.From, cm.Fingerprint) {
			fmt.Printf("[seguridad] fingerprint cambio para %s, posible suplantacion\n", cm.From)
			continue
		}
		if !n.markSeen(cm.ID) {
			continue
		}
		if cm.To != "" && cm.To != n.name && cm.From != n.name {
			continue
		}
		n.setConnectivity("connected")
		renderMessage(cm, n.name)
	}
}

func (n *Node) encryptPayload(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(n.roomSecret[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	cipherText := gcm.Seal(nil, nonce, plain, nil)
	env := encryptedMessage{
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		Cipher: base64.StdEncoding.EncodeToString(cipherText),
	}
	return json.Marshal(env)
}

func (n *Node) decryptPayload(raw []byte) ([]byte, error) {
	var env encryptedMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, err
	}
	cipherText, err := base64.StdEncoding.DecodeString(env.Cipher)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(n.roomSecret[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, cipherText, nil)
}

func (n *Node) markSeen(id string) bool {
	if strings.TrimSpace(id) == "" {
		return true
	}
	n.seenMu.Lock()
	defer n.seenMu.Unlock()

	if _, ok := n.seenMsgs[id]; ok {
		return false
	}
	n.seenMsgs[id] = time.Now()

	// Limpieza simple para evitar crecimiento infinito del mapa.
	if len(n.seenMsgs) > 5000 {
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, t := range n.seenMsgs {
			if t.Before(cutoff) {
				delete(n.seenMsgs, k)
			}
		}
	}
	return true
}

// --- Helpers de render ---

func renderMessage(msg chatMessage, myNick string) {
	t := time.Unix(msg.At, 0).Format("15:04:05")
	fromLabel := displayNickP2P(msg.From, myNick)
	toLabel := displayNickP2P(msg.To, myNick)
	switch msg.Type {
	case "system":
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
	case "chat":
		fmt.Printf("[%s] %s: %s\n", t, fromLabel, msg.Text)
	case "private":
		fmt.Printf("[%s] [privado] %s -> %s: %s\n", t, fromLabel, toLabel, msg.Text)
	case "reaction":
		fmt.Printf("[%s] %s reacciono %s\n", t, fromLabel, msg.Text)
	case "file":
		path, size, err := saveIncomingP2PFile(msg)
		if err != nil {
			fmt.Printf("[%s] %s envio archivo %s (no se pudo guardar: %v)\n", t, fromLabel, msg.FileName, err)
		} else {
			fmt.Printf("[%s] %s envio archivo %s (%d bytes) guardado en: %s\n", t, fromLabel, msg.FileName, size, path)
		}
	}
}

func displayNickP2P(nick, myNick string) string {
	n := strings.TrimSpace(nick)
	if n == "" {
		return colorizeName(nick)
	}
	label := colorizeName(n)
	if n == strings.TrimSpace(myNick) {
		return label + " (tu)"
	}
	return label
}

func clearConsoleP2P() {
	fmt.Print("\033[2J\033[H")
}

func (n *Node) reconnectLoop() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			if n.TopicPeerCount() > 0 || len(n.roomPeers) == 0 {
				continue
			}
			n.setConnectivity("reconnecting")
			n.ConnectToPeersWithRetry(n.roomPeers, 2, 500*time.Millisecond)
			if n.TopicPeerCount() == 0 {
				n.setConnectivity("degraded")
			}
		}
	}
}

func (n *Node) setConnectivity(state string) {
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	n.connectivity = state
	n.lastConnectivity = time.Now()
}

func (n *Node) connectivityState() string {
	n.statusMu.RLock()
	defer n.statusMu.RUnlock()
	if strings.TrimSpace(n.connectivity) == "" {
		return "connected"
	}
	return n.connectivity
}

func (n *Node) printDiag() {
	relay := "no"
	for _, addr := range n.host.Addrs() {
		if strings.Contains(addr.String(), "p2p-circuit") {
			relay = "si"
			break
		}
	}
	nat := "desconocido"
	for _, addr := range n.host.Addrs() {
		s := addr.String()
		if strings.Contains(s, "/ip4/10.") || strings.Contains(s, "/ip4/192.168.") || strings.Contains(s, "/ip4/172.") {
			nat = "probablemente detras de NAT"
			break
		}
	}
	if nat == "desconocido" && len(n.host.Addrs()) > 0 {
		nat = "posible publico"
	}
	fmt.Printf("Estado: %s\nPeers topic: %d\nRelay activo: %s\nNAT: %s\n", n.connectivityState(), n.TopicPeerCount(), relay, nat)
	for _, pid := range n.ps.ListPeers(n.topic.String()) {
		res := <-ping.Ping(n.ctx, n.host, pid)
		fmt.Printf("- %s RTT=%s\n", pid.String(), res.RTT)
	}
}

func (n *Node) fingerprint() string {
	sum := sha256.Sum256([]byte(n.host.ID().String()))
	return hex.EncodeToString(sum[:8])
}

func (n *Node) trackFingerprint(name, fp string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	prev, ok := n.peerFingerprints[name]
	if !ok {
		n.peerFingerprints[name] = fp
		return true
	}
	return prev == fp
}

type peerRate struct {
	tokens float64
	last   time.Time
}

func (n *Node) allowPeer(name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	pr := n.rate[name]
	if pr == nil {
		pr = &peerRate{tokens: 10, last: time.Now()}
		n.rate[name] = pr
	}
	now := time.Now()
	pr.tokens += now.Sub(pr.last).Seconds() * 5
	if pr.tokens > 10 {
		pr.tokens = 10
	}
	pr.last = now
	if pr.tokens < 1 {
		return false
	}
	pr.tokens--
	return true
}

func parseAtMentionP2P(line string) (to, text string, ok bool) {
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

func parsePrivate(line string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "/msg ")), " ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func isReaction(line string) bool {
	v := strings.TrimSpace(line)
	if strings.HasPrefix(v, ":") && strings.HasSuffix(v, ":") && len(v) > 2 {
		return true
	}
	return v == "+1" || v == "-1" || v == "ok"
}

func (n *Node) sendFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) > 1_500_000 {
		return fmt.Errorf("archivo excede 1.5MB")
	}
	name := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		name = path[idx+1:]
	}
	msg := chatMessage{
		ID:          newMessageID(),
		From:        n.name,
		Type:        "file",
		At:          time.Now().Unix(),
		FileName:    name,
		FilePayload: base64.StdEncoding.EncodeToString(raw),
		Fingerprint: n.fingerprint(),
	}
	if err := n.publishMessage(msg); err != nil {
		return err
	}
	t := time.Unix(msg.At, 0).Format("15:04:05")
	fmt.Printf("[%s] %s envio archivo %s (%d bytes)\n", t, displayNickP2P(msg.From, n.name), msg.FileName, len(raw))
	return nil
}

func saveIncomingP2PFile(msg chatMessage) (string, int, error) {
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return "", 0, err
	}
	if len(raw) > 1_500_000 {
		return "", 0, fmt.Errorf("archivo recibido excede limite")
	}
	name := sanitizeP2PFileName(msg.FileName)
	if name == "" {
		name = "archivo.bin"
	}
	dir, err := p2pDownloadDir()
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(dir, fmt.Sprintf("brinco-%d-%s", time.Now().Unix(), name))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", 0, err
	}
	return path, len(raw), nil
}

func p2pDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		downloads := filepath.Join(home, "Downloads")
		if st, serr := os.Stat(downloads); serr == nil && st.IsDir() {
			return downloads, nil
		}
	}
	return os.Getwd()
}

func sanitizeP2PFileName(name string) string {
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
	nickColorMuP2P.Lock()
	code, ok := nickColorByNameP2P[key]
	if !ok {
		code = nickColorCodesP2P[nextNickColorIdxP2P%len(nickColorCodesP2P)]
		nextNickColorIdxP2P++
		nickColorByNameP2P[key] = code
	}
	nickColorMuP2P.Unlock()
	return "\x1b[" + code + "m" + name + "\x1b[0m"
}

func colorizeSystem(text string) string {
	if !supportsColor() {
		return "* " + text
	}
	return "\x1b[96m* " + text + "\x1b[0m"
}

func supportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return strings.ToLower(os.Getenv("TERM")) != "dumb"
}

// --- mDNS notifee ---

type mdnsNotifee struct {
	host host.Host
	ctx  context.Context
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()
	_ = m.host.Connect(ctx, pi)
}

// --- Helpers de multiaddr ---

func peerInfosFromAddrs(addrs []string) []peer.AddrInfo {
	var pis []peer.AddrInfo
	for _, a := range addrs {
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		pis = append(pis, *pi)
	}
	return pis
}

// EnableRelayCircuit activa circuit relay v2 usando un relay propio
func (n *Node) EnableRelayCircuit(relayAddr string) error {
	if strings.TrimSpace(relayAddr) == "" {
		return nil
	}
	ma, err := multiaddr.NewMultiaddr(relayAddr)
	if err != nil {
		return fmt.Errorf("multiaddr invalida: %w", err)
	}
	pi, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return fmt.Errorf("peer info invalida: %w", err)
	}
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	if err := n.host.Connect(ctx, *pi); err != nil {
		return fmt.Errorf("no se pudo conectar al relay: %w", err)
	}
	_, err = client.Reserve(n.ctx, n.host, *pi)
	return err
}

func newMessageID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
