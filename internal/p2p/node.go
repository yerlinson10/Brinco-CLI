package p2p

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
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
	"strings"
	"sync"
	"time"

	"brinco-cli/internal/clipboard"
	"brinco-cli/internal/logx"
	"brinco-cli/internal/notify"
	"brinco-cli/internal/roomcode"
	"brinco-cli/internal/roomproto"
	"brinco-cli/internal/tui"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
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
	FromPeer    string `json:"fromPeer,omitempty"`
	Text        string `json:"text"`
	Type        string `json:"type"` // chat | system | private | reaction | file
	At          int64  `json:"at"`
	To          string `json:"to,omitempty"`
	ToPeer      string `json:"toPeer,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	FilePayload string `json:"filePayload,omitempty"`
	TransferID  string `json:"transferId,omitempty"`
	ChunkIndex  int    `json:"chunkIndex,omitempty"`
	ChunkCount  int    `json:"chunkCount,omitempty"`
	ChunkSize   int    `json:"chunkSize,omitempty"`
	TotalSize   int64  `json:"totalSize,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PubKey      string `json:"pubKey,omitempty"`
	DMNonce     string `json:"dmNonce,omitempty"`
	DMCipher    string `json:"dmCipher,omitempty"`
}

type encryptedMessage struct {
	Nonce  string `json:"nonce"`
	Cipher string `json:"cipher"`
}

// errNotEncryptedEnvelope indica que el JSON no es un sobre {"nonce","cipher"}
// valido para AES-GCM (p.ej. mensaje system en claro). receiveLoop puede hacer
// fallback a json.Unmarshal directo al chatMessage solo en ese caso.
var errNotEncryptedEnvelope = errors.New("payload no es sobre cifrado brinco")

type roomCodePayload struct {
	Topic  string   `msgpack:"t"`
	Relay  string   `msgpack:"r,omitempty"`
	Relays []string `msgpack:"y,omitempty"`
	Peers  []string `msgpack:"p,omitempty"`
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
	roomTopic string // topic brinco-... de la sala (para persistir preferencia de dial)

	seenMu   sync.Mutex
	seenMsgs map[string]time.Time

	statusMu         sync.RWMutex
	connectivity     string
	lastConnectivity time.Time
	peerFingerprints map[string]string
	rate             map[string]*peerRate
	peerNameByID     map[string]string
	peerIDByName     map[string]string
	peerPubByID      map[string]string
	roomCode         string
	roomSecret       [32]byte
	dmPriv           *ecdh.PrivateKey
	dmPubB64         string

	chatMu  sync.RWMutex
	chatApp *tui.App
}

var (
	nickColorCodesP2P = []string{
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
	maxP2PFileBytes     int64 = 10 * 1024 * 1024
	incomingP2PFileMu         sync.Mutex
	incomingP2PFiles          = map[string]*incomingP2PFileTransfer{}
)

type incomingP2PFileTransfer struct {
	name        string
	checksum    string
	totalSize   int64
	chunks      [][]byte
	got         []bool
	received    int
	updatedAt   time.Time
}

func SetMaxFileBytes(limit int64) {
	if limit > 0 {
		maxP2PFileBytes = limit
	}
}

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
	dmPriv, dmPubB64, err := generateDMKeys()
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error creando claves privadas: %w", err)
	}
	n := &Node{
		host:             h,
		ps:               ps,
		ctx:              retx,
		cancel:           cancel,
		seenMsgs:         make(map[string]time.Time),
		peerFingerprints: make(map[string]string),
		rate:             make(map[string]*peerRate),
		peerNameByID:     make(map[string]string),
		peerIDByName:     make(map[string]string),
		peerPubByID:      make(map[string]string),
		dmPriv:           dmPriv,
		dmPubB64:         dmPubB64,
	}
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
	dmPriv, dmPubB64, err := generateDMKeys()
	if err != nil {
		_ = h.Close()
		cancel()
		return nil, fmt.Errorf("error creando claves privadas: %w", err)
	}
	n := &Node{
		host:             h,
		ps:               ps,
		ctx:              ctx,
		cancel:           cancel,
		seenMsgs:         make(map[string]time.Time),
		peerFingerprints: make(map[string]string),
		rate:             make(map[string]*peerRate),
		peerNameByID:     make(map[string]string),
		peerIDByName:     make(map[string]string),
		peerPubByID:      make(map[string]string),
		dmPriv:           dmPriv,
		dmPubB64:         dmPubB64,
	}
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
	start := time.Now()
	for time.Now().Before(deadline) {
		for _, addr := range n.host.Addrs() {
			if strings.Contains(addr.String(), "p2p-circuit") {
				return true
			}
		}
		wait := relayWaitPollInterval(time.Since(start))
		if rem := time.Until(deadline); rem <= 0 {
			return false
		} else if wait > rem {
			wait = rem
		}
		select {
		case <-n.ctx.Done():
			return false
		case <-time.After(wait):
		}
	}
	return false
}

// relayWaitPollInterval usa sondeo mas frecuente al principio (el relay suele
// aparecer en los primeros segundos) y mas espaciado despues para no gastar CPU.
func relayWaitPollInterval(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < 5*time.Second:
		return 100 * time.Millisecond
	case elapsed < 15*time.Second:
		return 200 * time.Millisecond
	default:
		return 400 * time.Millisecond
	}
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
	if n.topic == nil {
		return fmt.Errorf("topic no inicializado, ejecuta JoinTopic primero")
	}
	msg.FromPeer = n.host.ID().String()
	msg.PubKey = n.dmPubB64
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

// SetRoomTopic fija el identificador de sala (topic) para memoria de dial local.
func (n *Node) SetRoomTopic(topic string) {
	n.roomTopic = strings.TrimSpace(topic)
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
	relays := splitRelayList(relayAddr)
	payload := roomCodePayload{Topic: topic, Relays: relays, Peers: peers}
	encoded, err := roomcode.Encode(payload)
	if err != nil {
		return "", err
	}
	return roomproto.Wrap(protocol, encoded), nil
}

func ParseRoomCodeDetailed(code string) (roomCodePayload, error) {
	protocol, encoded := roomproto.Unwrap(code)
	if protocol != "" && protocol != roomproto.ProtocolP2P && protocol != roomproto.ProtocolGuaranteed {
		return roomCodePayload{}, fmt.Errorf("codigo no es p2p")
	}
	if encoded == "" {
		encoded = strings.TrimSpace(code)
	}
	var rc roomCodePayload
	if err := roomcode.Decode(encoded, &rc); err != nil {
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
	return payload.Topic, firstString(payload.relayCandidates()), nil
}

func (p roomCodePayload) relayCandidates() []string {
	out := append([]string{}, p.Relays...)
	if strings.TrimSpace(p.Relay) != "" {
		out = append([]string{p.Relay}, out...)
	}
	return uniqueStrings(out)
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

// PickWorkingPeerAddrFromCandidates devuelve el multiaddr del primer candidato
// cuya identidad aparece en la malla del topic o tiene conexion libp2p activa.
func (n *Node) PickWorkingPeerAddrFromCandidates(candidates []string) string {
	self := n.host.ID()
	meshSet := make(map[peer.ID]struct{})
	if n.topic != nil && n.ps != nil {
		for _, p := range n.ps.ListPeers(n.topic.String()) {
			if p == self {
				continue
			}
			meshSet[p] = struct{}{}
		}
	}
	for _, addr := range candidates {
		s := strings.TrimSpace(addr)
		if s == "" {
			continue
		}
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		if pi.ID == self {
			continue
		}
		if _, ok := meshSet[pi.ID]; ok {
			return s
		}
	}
	for _, addr := range candidates {
		s := strings.TrimSpace(addr)
		if s == "" {
			continue
		}
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil || pi.ID == self {
			continue
		}
		if n.host.Network().Connectedness(pi.ID) == network.Connected {
			return s
		}
	}
	return ""
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

	app := tui.New(tui.Options{Title: "Brinco", Mode: "p2p", Nick: n.name})
	defer app.Shutdown()
	if err := app.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error UI: %v\n", err)
		return 1
	}
	n.chatMu.Lock()
	n.chatApp = app
	n.chatMu.Unlock()
	defer func() {
		n.chatMu.Lock()
		n.chatApp = nil
		n.chatMu.Unlock()
	}()

	go n.reconnectLoop()
	go n.cleanupIncomingTransfersLoop()
	go n.receiveLoop()

	app.PushLine("Conectado al topic de la sala")
	if n.TopicPeerCount() == 0 {
		app.PushLine("Aviso: aun no hay peers enlazados. Esperando conexiones...")
	}
	app.PushLine("Escribe mensajes y Enter para enviar")
	app.PushLine("Comandos: /code /peers /diag /clear /history !! @usuario mensaje | /msg u texto | /send archivo /quit /help")

	localLimiter := &peerRate{tokens: 3, last: time.Now()}
	history := make([]string, 0, 50)
	for {
		var line string
		var ok bool
		select {
		case <-n.ctx.Done():
			return 0
		case line, ok = <-app.Lines():
		}
		if !ok {
			_ = n.Publish(n.name+" salio", "system")
			app.ResetInterrupt()
			return 0
		}
		if line == "" {
			continue
		}
		if line == "!!" {
			if len(history) == 0 {
				app.PushLine("Historial vacio")
				continue
			}
			line = history[len(history)-1]
			app.PushLine("repite: " + line)
		}
		history = appendHistory(history, line, 40)

		if isReaction(line) {
			if !allowLocalP2P(localLimiter) {
				n.chatPushRateLimit()
				continue
			}
			reactionMsg := chatMessage{ID: newMessageID(), From: n.name, Text: line, Type: "reaction", At: time.Now().Unix(), Fingerprint: n.fingerprint(n.host.ID().String())}
			if err := n.publishMessage(reactionMsg); err != nil {
				fmt.Fprintf(os.Stderr, "Error enviando reaccion: %v\n", err)
			} else {
				for _, ln := range formatMessageLines(reactionMsg, n.name) {
					n.chatPushLine(ln)
				}
			}
			continue
		}

		if to, txt, ok := parseAtMentionP2P(line); ok {
			if !allowLocalP2P(localLimiter) {
				n.chatPushRateLimit()
				continue
			}
			pmMsg, err := n.newPrivateMessage(to, txt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error preparando privado: %v\n", err)
				continue
			}
			if err := n.publishMessage(pmMsg); err != nil {
				fmt.Fprintf(os.Stderr, "Error enviando privado: %v\n", err)
			} else {
				localPM := pmMsg
				localPM.Text = txt
				for _, ln := range formatMessageLines(localPM, n.name) {
					n.chatPushLine(ln)
				}
			}
			continue
		}

		if strings.HasPrefix(line, "/") {
			switch {
			case line == "/quit":
				_ = n.Publish(n.name+" salio", "system")
				app.Shutdown()
				return 0
			case line == "/code":
				for _, ln := range clipboard.RoomCodeFeedbackLines(roomCode) {
					app.PushLine(ln)
				}
			case line == "/peers":
				app.PushLine(fmt.Sprintf("Peers enlazados al topic: %d", n.TopicPeerCount()))
			case line == "/diag":
				n.printDiag()
			case line == "/help":
				app.PushLine("Comandos: /code /peers /diag /clear /history !! @usuario mensaje | /msg u texto | /send archivo /quit /help")
			case line == "/clear":
				n.chatPushClear()
			case line == "/history":
				n.chatPushHistory(formatP2PInputHistoryLines(history))
			case strings.HasPrefix(line, "/msg "):
				to, text, ok := parsePrivate(line)
				if !ok {
					app.PushLine("Uso: /msg <usuario> <texto>")
					continue
				}
				if !allowLocalP2P(localLimiter) {
					n.chatPushRateLimit()
					continue
				}
				pmMsg, err := n.newPrivateMessage(to, text)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error preparando privado: %v\n", err)
					continue
				}
				if err := n.publishMessage(pmMsg); err != nil {
					fmt.Fprintf(os.Stderr, "Error enviando privado: %v\n", err)
				} else {
					localPM := pmMsg
					localPM.Text = text
					for _, ln := range formatMessageLines(localPM, n.name) {
						n.chatPushLine(ln)
					}
				}
			case strings.HasPrefix(line, "/send "):
				if !allowLocalP2P(localLimiter) {
					n.chatPushRateLimit()
					continue
				}
				if err := n.sendFile(strings.TrimSpace(strings.TrimPrefix(line, "/send "))); err != nil {
					fmt.Fprintf(os.Stderr, "Error enviando archivo: %v\n", err)
				}
			case strings.HasPrefix(line, "/kick ") || strings.HasPrefix(line, "/mute ") || strings.HasPrefix(line, "/unmute ") || strings.HasPrefix(line, "/ban "):
				app.PushLine("Moderacion host no disponible en modo p2p/guaranteed (sin servidor central).")
			default:
				app.PushLine("Comando no reconocido. Usa /help")
			}
			continue
		}

		if !allowLocalP2P(localLimiter) {
			n.chatPushRateLimit()
			continue
		}
		if err := n.publishChatReliable(line); err != nil {
			fmt.Fprintf(os.Stderr, "Error enviando: %v\n", err)
		}
	}
}

func (n *Node) publishChatReliable(text string) error {
	msg := chatMessage{ID: newMessageID(), From: n.name, Text: text, Type: "chat", At: time.Now().Unix(), Fingerprint: n.fingerprint(n.host.ID().String())}
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
		for _, ln := range formatMessageLines(msg, n.name) {
			n.chatPushLine(ln)
		}
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
	if n.sub == nil {
		return
	}
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
		// Los mensajes cifrados son JSON {"nonce","cipher"}; si primero se decodifica
		// como chatMessage el unmarshal no falla (campos ignorados) y nunca se descifra.
		// Hay que intentar descifrado antes que JSON en claro.
		var cm chatMessage
		plain, derr := n.decryptPayload(msg.Data)
		switch {
		case derr == nil:
			if err := json.Unmarshal(plain, &cm); err != nil {
				continue
			}
		case errors.Is(derr, errNotEncryptedEnvelope):
			if err := json.Unmarshal(msg.Data, &cm); err != nil {
				continue
			}
		default:
			logx.Debug("mensaje p2p descartado (descifrado)", "err", derr)
			continue
		}
		senderPeerID := strings.TrimSpace(msg.GetFrom().String())
		if senderPeerID == "" {
			continue
		}
		if !n.bindPeerIdentity(senderPeerID, cm.From) {
			n.chatPushLine(fmt.Sprintf("[seguridad] alias '%s' en conflicto, posible suplantacion", strings.TrimSpace(cm.From)))
			continue
		}
		n.trackPeerPubKey(senderPeerID, cm.PubKey)
		if cm.Type != "file_chunk" && !n.allowPeer(senderPeerID) {
			continue
		}
		if cm.Fingerprint != "" && !n.trackFingerprint(senderPeerID, cm.Fingerprint) {
			n.chatPushLine(fmt.Sprintf("[seguridad] fingerprint cambio para %s, posible suplantacion", cm.From))
			continue
		}
		if !n.markSeen(cm.ID) {
			continue
		}
		if cm.Type == "private" {
			plain, err := n.decryptPrivatePayload(senderPeerID, cm)
			if err != nil {
				continue
			}
			cm.Text = plain
		}
		if cm.ToPeer != "" && cm.ToPeer != n.host.ID().String() && senderPeerID != n.host.ID().String() {
			continue
		}
		if cm.To != "" && cm.To != n.name && senderPeerID != n.host.ID().String() {
			continue
		}
		n.setConnectivity("connected")
		for _, ln := range formatMessageLines(cm, n.name) {
			n.chatPushLine(ln)
		}
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
		return nil, fmt.Errorf("%w: %w", errNotEncryptedEnvelope, err)
	}
	nNonce := strings.TrimSpace(env.Nonce)
	nCipher := strings.TrimSpace(env.Cipher)
	if nNonce == "" && nCipher == "" {
		return nil, errNotEncryptedEnvelope
	}
	if nNonce == "" || nCipher == "" {
		return nil, fmt.Errorf("sobre cifrado incompleto (nonce/cipher)")
	}
	nonce, err := base64.StdEncoding.DecodeString(nNonce)
	if err != nil {
		return nil, fmt.Errorf("nonce base64: %w", err)
	}
	cipherText, err := base64.StdEncoding.DecodeString(nCipher)
	if err != nil {
		return nil, fmt.Errorf("cipher base64: %w", err)
	}
	block, err := aes.NewCipher(n.roomSecret[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("tamano nonce invalido: %d (esperado %d)", len(nonce), gcm.NonceSize())
	}
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm open: %w", err)
	}
	return plain, nil
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

func formatMessageLines(msg chatMessage, myNick string) []string {
	t := time.Unix(msg.At, 0).Format("15:04:05")
	fromLabel := displayNickP2P(msg.From, myNick)
	toLabel := displayNickP2P(msg.To, myNick)
	switch msg.Type {
	case "system":
		triggerP2PNotification(msg, myNick)
		return []string{fmt.Sprintf("[%s] %s", t, colorizeSystem(msg.Text))}
	case "chat":
		triggerP2PNotification(msg, myNick)
		return []string{fmt.Sprintf("[%s] %s: %s", t, fromLabel, msg.Text)}
	case "private":
		triggerP2PNotification(msg, myNick)
		return []string{fmt.Sprintf("[%s] [privado] %s -> %s: %s", t, fromLabel, toLabel, msg.Text)}
	case "reaction":
		triggerP2PNotification(msg, myNick)
		return []string{fmt.Sprintf("[%s] %s reacciono %s", t, fromLabel, msg.Text)}
	case "file":
		path, size, err := saveIncomingP2PFile(msg)
		triggerP2PNotification(msg, myNick)
		if err != nil {
			return []string{fmt.Sprintf("[%s] %s envio archivo %s (no se pudo guardar: %v)", t, fromLabel, msg.FileName, err)}
		}
		return []string{fmt.Sprintf("[%s] %s envio archivo %s (%d bytes) guardado en: %s", t, fromLabel, msg.FileName, size, path)}
	case "file_chunk":
		result := saveIncomingP2PFileChunk(msg)
		if result.Err != nil {
			return []string{fmt.Sprintf("[%s] %s envio archivo %s (error: %v)", t, fromLabel, msg.FileName, result.Err)}
		}
		if result.Done {
			triggerP2PNotification(msg, myNick)
			return []string{fmt.Sprintf("[%s] %s envio archivo %s (%d bytes, sha256=%s) guardado en: %s", t, fromLabel, msg.FileName, result.Size, msg.Checksum, result.Path)}
		}
	}
	return nil
}

func (n *Node) chatPushLine(s string) {
	n.chatMu.RLock()
	app := n.chatApp
	n.chatMu.RUnlock()
	if app != nil {
		app.PushLine(s)
		return
	}
	fmt.Println(s)
}

func (n *Node) chatPushHistory(lines []string) {
	n.chatMu.RLock()
	app := n.chatApp
	n.chatMu.RUnlock()
	if app != nil {
		app.PushHistory(lines)
		return
	}
	for _, ln := range lines {
		fmt.Println(ln)
	}
}

func (n *Node) chatPushClear() {
	n.chatMu.RLock()
	app := n.chatApp
	n.chatMu.RUnlock()
	if app != nil {
		app.PushClear()
		return
	}
	clearConsoleP2P()
}

func (n *Node) chatPushRateLimit() {
	n.chatMu.RLock()
	app := n.chatApp
	n.chatMu.RUnlock()
	if app != nil && app.UsedTTY() {
		msg := "Rate limit: espera unos segundos antes de seguir enviando."
		if supportsColor() {
			app.PushLine("\x1b[91m" + msg + "\x1b[0m")
		} else {
			app.PushLine(msg)
		}
		return
	}
	printRateLimitLocalP2P()
}

func formatP2PInputHistoryLines(history []string) []string {
	if len(history) == 0 {
		return []string{"Historial vacio"}
	}
	start := 0
	if len(history) > 20 {
		start = len(history) - 20
	}
	out := make([]string, 0, len(history)-start)
	for i := start; i < len(history); i++ {
		out = append(out, fmt.Sprintf("%2d) %s", i-start+1, history[i]))
	}
	return out
}

func triggerP2PNotification(msg chatMessage, myNick string) {
	notify.Trigger(notify.Event{Type: msg.Type, From: msg.From, To: msg.To, Text: msg.Text, MyNick: myNick})
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

func (n *Node) formatDiagLines() []string {
	if n.topic == nil {
		return []string{
			fmt.Sprintf("Estado: %s", n.connectivityState()),
			"Peers topic: 0",
			"Relay activo: no",
			"NAT: desconocido",
		}
	}
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
	out := []string{
		fmt.Sprintf("Estado: %s", n.connectivityState()),
		fmt.Sprintf("Peers topic: %d", n.TopicPeerCount()),
		fmt.Sprintf("Relay activo: %s", relay),
		fmt.Sprintf("NAT: %s", nat),
	}
	for _, pid := range n.ps.ListPeers(n.topic.String()) {
		res := <-ping.Ping(n.ctx, n.host, pid)
		out = append(out, fmt.Sprintf("- %s RTT=%s", pid.String(), res.RTT))
	}
	return out
}

func (n *Node) printDiag() {
	for _, ln := range n.formatDiagLines() {
		n.chatPushLine(ln)
	}
}

func (n *Node) fingerprint(peerID string) string {
	sum := sha256.Sum256([]byte(peerID))
	return hex.EncodeToString(sum[:8])
}

func (n *Node) trackFingerprint(name, fp string) bool {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(fp) == "" {
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

func allowLocalP2P(pr *peerRate) bool {
	const maxTok = 3.0
	const refillPerSec = 0.55
	now := time.Now()
	pr.tokens += now.Sub(pr.last).Seconds() * refillPerSec
	if pr.tokens > maxTok {
		pr.tokens = maxTok
	}
	pr.last = now
	if pr.tokens < 1 {
		return false
	}
	pr.tokens--
	return true
}

func printRateLimitLocalP2P() {
	msg := "Rate limit: espera unos segundos antes de seguir enviando."
	if !supportsColor() {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	fmt.Fprintf(os.Stderr, "\x1b[91m%s\x1b[0m\n", msg)
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
	if int64(len(raw)) > maxP2PFileBytes {
		return fmt.Errorf("archivo excede limite de %s", formatBytes(maxP2PFileBytes))
	}
	name := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 {
		name = path[idx+1:]
	}
	const chunkSize = 64 * 1024
	sum := sha256.Sum256(raw)
	checksum := hex.EncodeToString(sum[:])
	transferID := checksum[:16]
	chunkCount := int((int64(len(raw)) + chunkSize - 1) / chunkSize)
	if chunkCount == 0 {
		chunkCount = 1
	}
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
		msg := chatMessage{
			ID:          newMessageID(),
			From:        n.name,
			Type:        "file_chunk",
			At:          time.Now().Unix(),
			FileName:    name,
			FilePayload: payload,
			TransferID:  transferID,
			ChunkIndex:  i,
			ChunkCount:  chunkCount,
			ChunkSize:   chunkSize,
			TotalSize:   int64(len(raw)),
			Checksum:    checksum,
			Fingerprint: n.fingerprint(n.host.ID().String()),
		}
		if err := n.publishMessage(msg); err != nil {
			return err
		}
	}
	t := time.Now().Format("15:04:05")
	n.chatPushLine(fmt.Sprintf("[%s] %s envio archivo %s (%s, sha256=%s)", t, displayNickP2P(n.name, n.name), name, formatBytes(int64(len(raw))), checksum))
	return nil
}

func saveIncomingP2PFile(msg chatMessage) (string, int, error) {
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return "", 0, err
	}
	if int64(len(raw)) > maxP2PFileBytes {
		return "", 0, fmt.Errorf("archivo recibido excede limite")
	}
	path, err := writeIncomingP2PFile(msg.FileName, raw)
	return path, len(raw), err
}

type p2pFileChunkResult struct {
	Path string
	Size int
	Done bool
	Err  error
}

func saveIncomingP2PFileChunk(msg chatMessage) p2pFileChunkResult {
	if !validP2PFileChunk(msg) {
		return p2pFileChunkResult{Err: fmt.Errorf("chunk invalido")}
	}
	raw, err := base64.StdEncoding.DecodeString(msg.FilePayload)
	if err != nil {
		return p2pFileChunkResult{Err: err}
	}
	incomingP2PFileMu.Lock()
	defer incomingP2PFileMu.Unlock()

	tr := incomingP2PFiles[msg.TransferID]
	if tr == nil {
		tr = &incomingP2PFileTransfer{
			name:      msg.FileName,
			checksum:  msg.Checksum,
			totalSize: msg.TotalSize,
			chunks:    make([][]byte, msg.ChunkCount),
			got:       make([]bool, msg.ChunkCount),
			updatedAt: time.Now(),
		}
		incomingP2PFiles[msg.TransferID] = tr
	}
	tr.updatedAt = time.Now()
	if msg.ChunkIndex >= len(tr.chunks) {
		return p2pFileChunkResult{Err: fmt.Errorf("chunk fuera de rango")}
	}
	if !tr.got[msg.ChunkIndex] {
		tr.got[msg.ChunkIndex] = true
		tr.chunks[msg.ChunkIndex] = raw
		tr.received++
	}
	if tr.received < len(tr.chunks) {
		return p2pFileChunkResult{}
	}
	assembled := make([]byte, 0, tr.totalSize)
	for _, chunk := range tr.chunks {
		assembled = append(assembled, chunk...)
	}
	if int64(len(assembled)) != tr.totalSize {
		return p2pFileChunkResult{Err: fmt.Errorf("tamano recibido invalido")}
	}
	sum := sha256.Sum256(assembled)
	if hex.EncodeToString(sum[:]) != tr.checksum {
		return p2pFileChunkResult{Err: fmt.Errorf("checksum invalido")}
	}
	path, err := writeIncomingP2PFile(tr.name, assembled)
	if err != nil {
		return p2pFileChunkResult{Err: err}
	}
	delete(incomingP2PFiles, msg.TransferID)
	return p2pFileChunkResult{Path: path, Size: len(assembled), Done: true}
}

func validP2PFileChunk(msg chatMessage) bool {
	return strings.TrimSpace(msg.TransferID) != "" &&
		strings.TrimSpace(msg.FileName) != "" &&
		msg.ChunkCount > 0 &&
		msg.ChunkIndex >= 0 &&
		msg.ChunkIndex < msg.ChunkCount &&
		msg.TotalSize >= 0 &&
		msg.TotalSize <= maxP2PFileBytes &&
		strings.TrimSpace(msg.Checksum) != "" &&
		int64(len(msg.FilePayload)) <= maxP2PFileBytes*2
}

func writeIncomingP2PFile(fileName string, raw []byte) (string, error) {
	name := sanitizeP2PFileName(fileName)
	if name == "" {
		name = "archivo.bin"
	}
	dir, err := p2pDownloadDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("brinco-%d-%s", time.Now().Unix(), name))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
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

func splitRelayList(value string) []string {
	return uniqueStrings(strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	}))
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

func selectBestP2PRelay(relays []string, timeout time.Duration) string {
	relays = uniqueStrings(relays)
	if len(relays) == 0 {
		return ""
	}
	best := ""
	var bestRTT time.Duration
	for _, relay := range relays {
		tcpAddr, ok := multiaddrTCPAddr(relay)
		if !ok {
			continue
		}
		start := time.Now()
		conn, err := netDialTimeout("tcp", tcpAddr, timeout)
		if err != nil {
			fmt.Printf("relay %s no disponible (%v)\n", relay, err)
			continue
		}
		_ = conn.Close()
		rtt := time.Since(start)
		fmt.Printf("relay %s disponible (%s)\n", relay, rtt.Round(time.Millisecond))
		if best == "" || rtt < bestRTT {
			best = relay
			bestRTT = rtt
		}
	}
	if best == "" {
		return relays[0]
	}
	return best
}

var netDialTimeout = func(network, address string, timeout time.Duration) (interface{ Close() error }, error) {
	return (&net.Dialer{Timeout: timeout}).Dial(network, address)
}

func multiaddrTCPAddr(value string) (string, bool) {
	ma, err := multiaddr.NewMultiaddr(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	host, err := ma.ValueForProtocol(multiaddr.P_IP4)
	if err != nil {
		host, err = ma.ValueForProtocol(multiaddr.P_DNS4)
	}
	if err != nil {
		host, err = ma.ValueForProtocol(multiaddr.P_DNS)
	}
	port, err := ma.ValueForProtocol(multiaddr.P_TCP)
	if err != nil || host == "" || port == "" {
		return "", false
	}
	return host + ":" + port, true
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

func generateDMKeys() (*ecdh.PrivateKey, string, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	return priv, base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

func (n *Node) bindPeerIdentity(peerID, alias string) bool {
	peerID = strings.TrimSpace(peerID)
	alias = strings.TrimSpace(alias)
	if peerID == "" {
		return false
	}

	n.statusMu.Lock()
	if prevAlias, ok := n.peerNameByID[peerID]; ok && alias != "" && prevAlias != alias {
		n.statusMu.Unlock()
		return false
	}
	var staleHolder string
	if alias != "" {
		if prevID, ok := n.peerIDByName[alias]; ok && prevID != peerID {
			staleHolder = prevID
		}
	}
	n.statusMu.Unlock()

	if staleHolder != "" && n.peerIdentityActive(staleHolder) {
		return false
	}

	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	if prevAlias, ok := n.peerNameByID[peerID]; ok && alias != "" && prevAlias != alias {
		return false
	}
	if alias != "" {
		if prevID, ok := n.peerIDByName[alias]; ok && prevID != peerID {
			if n.peerIdentityActive(prevID) {
				return false
			}
			n.unbindPeerLocked(prevID)
		}
		n.peerIDByName[alias] = peerID
		n.peerNameByID[peerID] = alias
	}
	return true
}

// peerIdentityActive indica si el peer sigue presente en la malla del topic
// o con conexion libp2p activa. Si no, otro cliente puede reclamar el mismo
// alias (reconexion tras cerrar la consola sin suplantacion real).
func (n *Node) peerIdentityActive(peerIDStr string) bool {
	peerIDStr = strings.TrimSpace(peerIDStr)
	if peerIDStr == "" {
		return false
	}
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return false
	}
	if n.host.Network().Connectedness(pid) == network.Connected {
		return true
	}
	if n.topic != nil && n.ps != nil {
		topic := n.topic.String()
		for _, p := range n.ps.ListPeers(topic) {
			if p == pid {
				return true
			}
		}
	}
	return false
}

func (n *Node) unbindPeerLocked(peerID string) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return
	}
	if alias, ok := n.peerNameByID[peerID]; ok && alias != "" {
		if n.peerIDByName[alias] == peerID {
			delete(n.peerIDByName, alias)
		}
	}
	delete(n.peerNameByID, peerID)
	delete(n.peerFingerprints, peerID)
	delete(n.peerPubByID, peerID)
	delete(n.rate, peerID)
}

func (n *Node) trackPeerPubKey(peerID, pubKey string) {
	peerID = strings.TrimSpace(peerID)
	pubKey = strings.TrimSpace(pubKey)
	if peerID == "" || pubKey == "" {
		return
	}
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	if _, ok := n.peerPubByID[peerID]; !ok {
		n.peerPubByID[peerID] = pubKey
	}
}

func (n *Node) lookupPeerByAlias(alias string) (string, string, bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", "", false
	}
	n.statusMu.RLock()
	defer n.statusMu.RUnlock()
	peerID := n.peerIDByName[alias]
	pubKey := n.peerPubByID[peerID]
	if peerID == "" || pubKey == "" {
		return "", "", false
	}
	return peerID, pubKey, true
}

func (n *Node) newPrivateMessage(toAlias, text string) (chatMessage, error) {
	toPeerID, toPub, ok := n.lookupPeerByAlias(toAlias)
	if !ok {
		return chatMessage{}, fmt.Errorf("usuario '%s' no descubierto aun; espera su primer mensaje en sala", toAlias)
	}
	nonceB64, cipherB64, err := n.encryptPrivatePayload(toPub, text)
	if err != nil {
		return chatMessage{}, err
	}
	return chatMessage{
		ID:          newMessageID(),
		From:        n.name,
		To:          toAlias,
		ToPeer:      toPeerID,
		Type:        "private",
		At:          time.Now().Unix(),
		Fingerprint: n.fingerprint(n.host.ID().String()),
		DMNonce:     nonceB64,
		DMCipher:    cipherB64,
	}, nil
}

func (n *Node) encryptPrivatePayload(peerPubB64, text string) (string, string, error) {
	peerPubRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(peerPubB64))
	if err != nil {
		return "", "", fmt.Errorf("clave publica destinatario invalida: %w", err)
	}
	curve := ecdh.X25519()
	peerPub, err := curve.NewPublicKey(peerPubRaw)
	if err != nil {
		return "", "", fmt.Errorf("clave publica destinatario invalida: %w", err)
	}
	secret, err := n.dmPriv.ECDH(peerPub)
	if err != nil {
		return "", "", fmt.Errorf("no se pudo derivar secreto privado: %w", err)
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(text), nil)
	return base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(sealed), nil
}

func (n *Node) decryptPrivatePayload(senderPeerID string, msg chatMessage) (string, error) {
	if strings.TrimSpace(msg.DMNonce) == "" || strings.TrimSpace(msg.DMCipher) == "" {
		return "", fmt.Errorf("mensaje privado sin payload cifrado")
	}
	if strings.TrimSpace(msg.ToPeer) != "" && strings.TrimSpace(msg.ToPeer) != n.host.ID().String() {
		return "", fmt.Errorf("mensaje privado no dirigido a este nodo")
	}
	n.statusMu.RLock()
	senderPubB64 := n.peerPubByID[senderPeerID]
	n.statusMu.RUnlock()
	if strings.TrimSpace(senderPubB64) == "" {
		return "", fmt.Errorf("clave publica remitente desconocida")
	}
	senderPubRaw, err := base64.StdEncoding.DecodeString(senderPubB64)
	if err != nil {
		return "", err
	}
	curve := ecdh.X25519()
	senderPub, err := curve.NewPublicKey(senderPubRaw)
	if err != nil {
		return "", err
	}
	secret, err := n.dmPriv.ECDH(senderPub)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(msg.DMNonce)
	if err != nil {
		return "", err
	}
	cipherText, err := base64.StdEncoding.DecodeString(msg.DMCipher)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (n *Node) cleanupIncomingTransfersLoop() {
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			cleanupStaleIncomingP2PTransfers(5 * time.Minute)
		}
	}
}

func cleanupStaleIncomingP2PTransfers(maxIdle time.Duration) {
	cutoff := time.Now().Add(-maxIdle)
	incomingP2PFileMu.Lock()
	defer incomingP2PFileMu.Unlock()
	for id, tr := range incomingP2PFiles {
		if tr.updatedAt.Before(cutoff) {
			delete(incomingP2PFiles, id)
		}
	}
}
