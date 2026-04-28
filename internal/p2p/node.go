package p2p

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
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
	ID   string `json:"id,omitempty"`
	From string `json:"from"`
	Text string `json:"text"`
	Type string `json:"type"` // chat | system
	At   int64  `json:"at"`
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

	return &Node{host: h, ps: ps, ctx: retx, cancel: cancel, seenMsgs: make(map[string]time.Time)}, nil
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

	return &Node{host: h, ps: ps, ctx: ctx, cancel: cancel, seenMsgs: make(map[string]time.Time)}, nil
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
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
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
	go n.receiveLoop()

	fmt.Println("Conectado al topic de la sala")
	if n.TopicPeerCount() == 0 {
		fmt.Println("Aviso: aun no hay peers enlazados. Esperando conexiones...")
	}
	fmt.Println("Escribe mensajes y Enter para enviar")
	fmt.Println("Comandos: /code /peers /quit /help")

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

		if strings.HasPrefix(line, "/") {
			switch line {
			case "/quit":
				_ = n.Publish(n.name+" salio", "system")
				return 0
			case "/code":
				fmt.Printf("Codigo de sala: %s\n", roomCode)
			case "/peers":
				fmt.Printf("Peers enlazados al topic: %d\n", n.TopicPeerCount())
			case "/help":
				fmt.Println("Comandos: /code /peers /quit /help")
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
	msg := chatMessage{ID: newMessageID(), From: n.name, Text: text, Type: "chat", At: time.Now().Unix()}
	var lastErr error
	for i := 0; i < 80; i++ {
		if n.TopicPeerCount() == 0 && len(n.roomPeers) > 0 {
			n.ConnectToPeers(n.roomPeers)
		}
		if err := n.publishMessage(msg); err != nil {
			lastErr = err
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return nil
	}
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
		// No mostrar mensajes propios
		if msg.ReceivedFrom == n.host.ID() {
			continue
		}
		var cm chatMessage
		if err := json.Unmarshal(msg.Data, &cm); err != nil {
			continue
		}
		if !n.markSeen(cm.ID) {
			continue
		}
		renderMessage(cm)
	}
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

func renderMessage(msg chatMessage) {
	t := time.Unix(msg.At, 0).Format("15:04:05")
	switch msg.Type {
	case "system":
		fmt.Printf("[%s] %s\n", t, colorizeSystem(msg.Text))
	case "chat":
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
