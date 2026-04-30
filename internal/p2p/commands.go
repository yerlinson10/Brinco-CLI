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

// RunCreate crea una sala nueva usando libp2p + red publica IPFS
// Si se pasa relayAddr, usa ese relay propio ademas de la red publica
func RunCreate(name, relayAddr string) int {
	logx.Info("p2p create start", "relay", relayAddr)
	if strings.TrimSpace(name) == "" {
		name = "host"
	}

	fmt.Println("Iniciando nodo P2P...")

	relayAddrs := splitRelayList(relayAddr)

	ctx := context.Background()
	node, err := NewNode(ctx, relayAddrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P...")
	node.Bootstrap(relayAddrs)

	if bestRelay := selectBestP2PRelay(relayAddrs, 900*time.Millisecond); bestRelay != "" {
		if err := node.EnableRelayCircuit(bestRelay); err != nil {
			fmt.Fprintf(os.Stderr, "Advertencia: no se pudo reservar circuito relay: %v\n", err)
		} else {
			fmt.Printf("Relay propio activado: %s\n", bestRelay)
		}
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

	code, err := BuildRoomCode(topic, strings.Join(relayAddrs, ","), node.AdvertisePeerAddrs())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando codigo: %v\n", err)
		return 1
	}

	_ = saveLastCode(code)

	fmt.Printf("\nSala creada exitosamente\n")
	fmt.Printf("Codigo de sala: %s\n", code)
	clipboard.AnnounceRoomCode(code)
	fmt.Printf("Peer ID:        %s\n", node.ID())
	fmt.Println("Comparte el codigo con tus peers")
	fmt.Println()
	logx.Info("p2p sala creada", "peerID", node.ID())

	_ = node.Publish(name+" creo la sala", "system")
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
	ctx := context.Background()
	node, err := NewNode(ctx, relayAddrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P...")
	node.Bootstrap(relayAddrs)

	if bestRelay := selectBestP2PRelay(relayAddrs, 900*time.Millisecond); bestRelay != "" {
		if err := node.EnableRelayCircuit(bestRelay); err != nil {
			fmt.Fprintf(os.Stderr, "Advertencia: relay no disponible: %v\n", err)
		} else {
			fmt.Printf("Relay propio activado: %s\n", bestRelay)
		}
	}

	if err := node.JoinTopic(topic); err != nil {
		fmt.Fprintf(os.Stderr, "Error uniendose al topic: %v\n", err)
		return 1
	}

	if len(payload.Peers) > 0 {
		fmt.Printf("Intentando enlazar con %d peer(s) de la sala...\n", len(payload.Peers))
		node.SetRoomPeers(payload.Peers)
		node.ConnectToPeersWithRetry(payload.Peers, 5, 700*time.Millisecond)
		if node.TopicPeerCount() == 0 {
			fmt.Println("Aviso: no se logro enlace directo con peers. El chat puede no enviar/recibir sin relay o puertos abiertos.")
			logx.Warn("p2p join sin peers enlazados")
		}
	}

	_ = node.Publish(name+" se unio", "system")
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
	ctx := context.Background()
	node, err := NewNodeGuaranteed(ctx, nil)
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
	fmt.Print("Esperando direccion relay")
	gotRelay := node.WaitForRelay(30 * time.Second)
	fmt.Println()
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

	_ = saveLastCode(code)

	fmt.Printf("\nSala creada exitosamente (modo guaranteed)\n")
	fmt.Printf("Codigo de sala: %s\n", code)
	clipboard.AnnounceRoomCode(code)
	fmt.Printf("Peer ID:        %s\n", node.ID())
	fmt.Println("Comparte el codigo con tus peers")
	fmt.Println()
	logx.Info("p2p guaranteed sala creada", "peerID", node.ID())

	_ = node.Publish(name+" creo la sala", "system")
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
	ctx := context.Background()
	node, err := NewNodeGuaranteed(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creando nodo: %v\n", err)
		return 1
	}
	defer node.Close()

	node.SetName(name)

	fmt.Println("Conectando a la red P2P y buscando relays...")
	node.Bootstrap(nil)

	// Esperar relay para poder ser alcanzado por el otro peer.
	fmt.Print("Esperando direccion relay")
	gotRelay := node.WaitForRelay(30 * time.Second)
	fmt.Println()
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
		node.ConnectToPeersWithRetry(payload.Peers, 8, 1*time.Second)
		if node.TopicPeerCount() == 0 {
			fmt.Println("Aviso: sin enlace aun. El relay negociara la conexion en segundo plano...")
			logx.Warn("guaranteed join sin peers directos tras reintentos")
		} else {
			fmt.Printf("Enlazado con %d peer(s)\n", node.TopicPeerCount())
		}
	}

	_ = node.Publish(name+" se unio", "system")
	return node.RunChat(code)
}

// LoadLastCode devuelve el ultimo codigo de sala generado
func LoadLastCode() (string, error) {
	return loadLastCode()
}
