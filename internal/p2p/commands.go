package p2p

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"brinco-cli/internal/clipboard"
	"brinco-cli/internal/logx"
)

const (
	defaultRelayProbeTimeout        = 900 * time.Millisecond
	defaultWaitRelayTimeout         = 30 * time.Second
	defaultJoinConnectRetries       = 5
	defaultJoinConnectRetryDelay    = 700 * time.Millisecond
	guaranteedJoinConnectRetries    = 8
	guaranteedJoinConnectRetryDelay = 1 * time.Second
)

func logNonFatal(action string, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Advertencia: %s: %v\n", action, err)
	logx.Warn("p2p non-fatal", "action", action, "err", err)
}

func persistRoomCode(code string) {
	logNonFatal("no se pudo guardar el ultimo codigo", saveLastCode(code))
}

func publishSystemEvent(node *Node, message string) {
	logNonFatal("no se pudo publicar mensaje de sistema", node.Publish(message, "system"))
}

func newNodeWithDefaults(guaranteed bool, relayAddrs []string) (*Node, error) {
	ctx := context.Background()
	nodeFactory := NewNode
	if guaranteed {
		nodeFactory = NewNodeGuaranteed
	}
	return nodeFactory(ctx, relayAddrs)
}

func pickRelayAndEnable(node *Node, relayAddrs []string) {
	bestRelay := selectBestP2PRelay(relayAddrs, defaultRelayProbeTimeout)
	if bestRelay == "" {
		return
	}
	if err := node.EnableRelayCircuit(bestRelay); err != nil {
		fmt.Fprintf(os.Stderr, "Advertencia: relay no disponible: %v\n", err)
		return
	}
	fmt.Printf("Relay propio activado: %s\n", bestRelay)
}

func waitForRelayOrWarn(node *Node) bool {
	fmt.Print("Esperando direccion relay")
	gotRelay := node.WaitForRelay(defaultWaitRelayTimeout)
	fmt.Println()
	return gotRelay
}

// RunCreate crea una sala nueva usando libp2p + red publica IPFS
// Si se pasa relayAddr, usa ese relay propio ademas de la red publica
func RunCreate(name, relayAddr string) int {
	logx.Info("p2p create start", "relay", relayAddr)
	if strings.TrimSpace(name) == "" {
		name = "host"
	}

	fmt.Println("Iniciando nodo P2P...")

	relayAddrs := splitRelayList(relayAddr)

	node, err := newNodeWithDefaults(false, relayAddrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P...")
	node.Bootstrap(relayAddrs)

	pickRelayAndEnable(node, relayAddrs)

	topic, err := RandomTopic()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando sala: %v\n", err)
		return 1
	}

	if err := node.JoinTopic(topic); err != nil {
		fmt.Fprintf(os.Stderr, "Error uniendose al topic: %v\n", err)
		return 1
	}

	code, err := BuildRoomCode(topic, strings.Join(relayAddrs, ","), node.AdvertisePeerAddrs())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando codigo: %v\n", err)
		return 1
	}

	persistRoomCode(code)

	fmt.Printf("\nSala creada exitosamente\n")
	fmt.Printf("Codigo de sala: %s\n", code)
	clipboard.AnnounceRoomCode(code)
	fmt.Printf("Peer ID:        %s\n", node.ID())
	fmt.Println("Comparte el codigo con tus peers")
	fmt.Println()
	logx.Info("p2p sala creada", "peerID", node.ID())

	publishSystemEvent(node, name+" creo la sala")
	return node.RunChat(code)
}

// RunJoin se une a una sala usando el codigo generado por RunCreate
func RunJoin(name, code, relayAddr string) int {
	logx.Info("p2p join start", "relay", relayAddr)
	if strings.TrimSpace(name) == "" {
		name = "guest"
	}
	if strings.TrimSpace(code) == "" {
		fmt.Fprintln(os.Stderr, "Error: --code es obligatorio")
		return 1
	}

	payload, err := ParseRoomCodeDetailed(code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parseando codigo: %v\n", err)
		return 1
	}

	topic := payload.Topic
	codeRelays := payload.relayCandidates()

	// El relay del codigo tiene prioridad; si el usuario tambien paso --relay, lo agrega
	relayAddrs := codeRelays
	if cliRelays := splitRelayList(relayAddr); len(cliRelays) > 0 {
		relayAddrs = append(cliRelays, relayAddrs...)
	}
	relayAddrs = uniqueStrings(relayAddrs)

	fmt.Println("Iniciando nodo P2P...")
	node, err := newNodeWithDefaults(false, relayAddrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P...")
	node.Bootstrap(relayAddrs)

	pickRelayAndEnable(node, relayAddrs)

	if err := node.JoinTopic(topic); err != nil {
		fmt.Fprintf(os.Stderr, "Error uniendose al topic: %v\n", err)
		return 1
	}

	if len(payload.Peers) > 0 {
		fmt.Printf("Intentando enlazar con %d peer(s) de la sala...\n", len(payload.Peers))
		node.SetRoomPeers(payload.Peers)
		node.ConnectToPeersWithRetry(payload.Peers, defaultJoinConnectRetries, defaultJoinConnectRetryDelay)
		if node.TopicPeerCount() == 0 {
			fmt.Println("Aviso: no se logro enlace directo con peers. El chat puede no enviar/recibir sin relay o puertos abiertos.")
			logx.Warn("p2p join sin peers enlazados")
		}
	}

	publishSystemEvent(node, name+" se unio")
	return node.RunChat(code)
}

// RunCreateGuaranteed crea una sala usando libp2p con relay automatico de la red IPFS.
// No requiere relay manual: descubre relays dinamicamente via DHT.
func RunCreateGuaranteed(name string) int {
	logx.Info("p2p guaranteed create start")
	if strings.TrimSpace(name) == "" {
		name = "host"
	}

	fmt.Println("Iniciando nodo P2P (modo guaranteed)...")
	node, err := newNodeWithDefaults(true, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P y buscando relays...")
	node.Bootstrap(nil)

	// Esperar hasta 30 segundos a que AutoRelay reserve un slot en un relay.
	// Esto es clave: sin relay address en el codigo, el otro peer no podra conectar.
	gotRelay := waitForRelayOrWarn(node)
	if gotRelay {
		fmt.Println("Relay obtenido exitosamente")
		logx.Info("guaranteed: relay address obtenida")
	} else {
		fmt.Println("Aviso: no se obtuvo relay. Se usaran direcciones directas.")
		logx.Warn("guaranteed: timeout esperando relay")
	}

	topic, err := RandomTopic()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando sala: %v\n", err)
		return 1
	}

	if err := node.JoinTopic(topic); err != nil {
		fmt.Fprintf(os.Stderr, "Error uniendose al topic: %v\n", err)
		return 1
	}

	code, err := BuildRoomCodeWithProtocol(topic, "", node.AdvertisePeerAddrs(), "guaranteed")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando codigo: %v\n", err)
		return 1
	}

	persistRoomCode(code)

	fmt.Printf("\nSala creada exitosamente (modo guaranteed)\n")
	fmt.Printf("Codigo de sala: %s\n", code)
	clipboard.AnnounceRoomCode(code)
	fmt.Printf("Peer ID:        %s\n", node.ID())
	fmt.Println("Comparte el codigo con tus peers")
	fmt.Println()
	logx.Info("p2p guaranteed sala creada", "peerID", node.ID())

	publishSystemEvent(node, name+" creo la sala")
	return node.RunChat(code)
}

// RunJoinGuaranteed se une a una sala en modo guaranteed usando libp2p con relay
// automatico via DHT de la red IPFS. No requiere relay manual.
func RunJoinGuaranteed(name, code string) int {
	logx.Info("p2p guaranteed join start")
	if strings.TrimSpace(name) == "" {
		name = "guest"
	}
	if strings.TrimSpace(code) == "" {
		fmt.Fprintln(os.Stderr, "Error: --code es obligatorio")
		return 1
	}

	payload, err := ParseRoomCodeDetailed(code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parseando codigo: %v\n", err)
		return 1
	}

	fmt.Println("Iniciando nodo P2P (modo guaranteed)...")
	node, err := newNodeWithDefaults(true, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P y buscando relays...")
	node.Bootstrap(nil)

	// Esperar relay para poder ser alcanzado por el otro peer.
	gotRelay := waitForRelayOrWarn(node)
	if gotRelay {
		fmt.Println("Relay obtenido exitosamente")
		logx.Info("guaranteed join: relay address obtenida")
	} else {
		fmt.Println("Aviso: no se obtuvo relay. Intentando conexion directa...")
		logx.Warn("guaranteed join: timeout esperando relay")
	}

	if err := node.JoinTopic(payload.Topic); err != nil {
		fmt.Fprintf(os.Stderr, "Error uniendose al topic: %v\n", err)
		return 1
	}

	if len(payload.Peers) > 0 {
		fmt.Printf("Intentando enlazar con %d peer(s) de la sala...\n", len(payload.Peers))
		node.SetRoomPeers(payload.Peers)
		node.ConnectToPeersWithRetry(payload.Peers, guaranteedJoinConnectRetries, guaranteedJoinConnectRetryDelay)
		if node.TopicPeerCount() == 0 {
			fmt.Println("Aviso: sin enlace aun. El relay negociara la conexion en segundo plano...")
			logx.Warn("guaranteed join sin peers directos tras reintentos")
		} else {
			fmt.Printf("Enlazado con %d peer(s)\n", node.TopicPeerCount())
		}
	}

	publishSystemEvent(node, name+" se unio")
	return node.RunChat(code)
}

// LoadLastCode devuelve el ultimo codigo de sala generado
func LoadLastCode() (string, error) {
	return loadLastCode()
}
