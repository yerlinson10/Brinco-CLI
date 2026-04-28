package p2p

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunCreate crea una sala nueva usando libp2p + red publica IPFS
// Si se pasa relayAddr, usa ese relay propio ademas de la red publica
func RunCreate(name, relayAddr string) int {
	if strings.TrimSpace(name) == "" {
		name = "host"
	}

	fmt.Println("Iniciando nodo P2P...")

	var relayAddrs []string
	if strings.TrimSpace(relayAddr) != "" {
		relayAddrs = append(relayAddrs, relayAddr)
	}

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

	if strings.TrimSpace(relayAddr) != "" {
		if err := node.EnableRelayCircuit(relayAddr); err != nil {
			fmt.Fprintf(os.Stderr, "Advertencia: no se pudo reservar circuito relay: %v\n", err)
		} else {
			fmt.Println("Relay propio activado")
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

	code, err := BuildRoomCode(topic, relayAddr, node.AdvertisePeerAddrs())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando codigo: %v\n", err)
		return 1
	}

	_ = saveLastCode(code)

	fmt.Printf("\nSala creada exitosamente\n")
	fmt.Printf("Codigo de sala: %s\n", code)
	fmt.Printf("Peer ID:        %s\n", node.ID())
	fmt.Println("Comparte el codigo con tus peers")
	fmt.Println()

	_ = node.Publish(name+" creo la sala", "system")
	return node.RunChat(code)
}

// RunJoin se une a una sala usando el codigo generado por RunCreate
func RunJoin(name, code, relayAddr string) int {
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
	codeRelay := payload.Relay

	// El relay del codigo tiene prioridad; si el usuario tambien paso --relay, lo agrega
	effectiveRelay := codeRelay
	if strings.TrimSpace(relayAddr) != "" {
		effectiveRelay = relayAddr
	}

	var relayAddrs []string
	if strings.TrimSpace(effectiveRelay) != "" {
		relayAddrs = append(relayAddrs, effectiveRelay)
	}

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

	if strings.TrimSpace(effectiveRelay) != "" {
		if err := node.EnableRelayCircuit(effectiveRelay); err != nil {
			fmt.Fprintf(os.Stderr, "Advertencia: relay no disponible: %v\n", err)
		} else {
			fmt.Println("Relay propio activado")
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
	}

	_ = node.Publish(name+" se unio", "system")
	return node.RunChat(code)
}

// LoadLastCode devuelve el ultimo codigo de sala generado
func LoadLastCode() (string, error) {
	return loadLastCode()
}
