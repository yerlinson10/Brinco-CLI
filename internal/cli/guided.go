package cli

import (
	"fmt"
	"net"
	"os"
	"strings"

	"brinco-cli/internal/chat"
	p2pcmd "brinco-cli/internal/p2p"
)

func runHostGuided() int {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Crear sala (asistente) ===")
	fmt.Fprintln(os.Stderr, "Enter acepta el valor entre [corchetes].")
	fmt.Fprintln(os.Stderr, "")

	name, err := readLineTrimOrDefault("Nombre visible [host]: ", "host")
	if err != nil {
		return 1
	}
	mode, err := readLineTrimOrDefault("Modo: p2p | direct | relay | guaranteed [p2p]: ", "p2p")
	if err != nil {
		return 1
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "p2p"
	}
	switch mode {
	case "p2p", "direct", "relay", "guaranteed":
	default:
		fmt.Fprintf(os.Stderr, "Modo desconocido %q; uso p2p.\n", mode)
		mode = "p2p"
	}

	direct := mode == "direct"
	relay := ""
	listen := "0.0.0.0:9090"
	public := ""
	password := ""

	switch mode {
	case "relay":
		relay, err = readLineTrimOrDefault("Direccion del relay TCP host:puerto [127.0.0.1:10000]: ", "127.0.0.1:10000")
		if err != nil {
			return 1
		}
		if strings.TrimSpace(relay) == "" {
			fmt.Fprintln(os.Stderr, "Error: relay obligatorio en modo relay")
			return 1
		}
		password, err = readOptionalPasswordLine()
		if err != nil {
			return 1
		}
	case "direct":
		listen, err = readLineTrimOrDefault("Escucha local (listen) [0.0.0.0:9090]: ", "0.0.0.0:9090")
		if err != nil {
			return 1
		}
		public, err = readLineTrim("Host:puerto publico (--public, obligatorio si escuchas en 0.0.0.0): ")
		if err != nil {
			return 1
		}
		if isWildcardListen(listen) && strings.TrimSpace(public) == "" {
			fmt.Fprintln(os.Stderr, "Error: con 0.0.0.0 debes indicar host:puerto publico")
			return 1
		}
		useRelay, err := readLineTrimOrDefault("Crear sala sobre relay TCP en vez de servidor propio? (s/N) [N]: ", "n")
		if err != nil {
			return 1
		}
		if strings.EqualFold(strings.TrimSpace(useRelay), "s") || strings.EqualFold(strings.TrimSpace(useRelay), "si") {
			relay, err = readLineTrimOrDefault("Relay host:puerto [127.0.0.1:10000]: ", "127.0.0.1:10000")
			if err != nil {
				return 1
			}
			if strings.TrimSpace(relay) == "" {
				fmt.Fprintln(os.Stderr, "Error: relay obligatorio si eliges relay")
				return 1
			}
			mode = "direct"
			direct = true
		}
		password, err = readOptionalPasswordLine()
		if err != nil {
			return 1
		}
	case "p2p":
		relay, err = readLineTrim("Relay libp2p multiaddr (opcional, Enter vacio): ")
		if err != nil {
			return 1
		}
	}

	return execRoomCreate(name, mode, relay, direct, listen, public, password)
}

func runJoinGuided() int {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Unirse a sala (asistente) ===")
	fmt.Fprintln(os.Stderr, "")

	code, err := readLineTrim("Codigo de sala (Enter para usar el ultimo guardado en disco): ")
	if err != nil {
		return 1
	}
	code = strings.TrimSpace(code)
	if code == "" {
		var e error
		code, e = loadAnyLastRoomCode()
		if e != nil || strings.TrimSpace(code) == "" {
			fmt.Fprintf(os.Stderr, "Error: %v\n", e)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Usando codigo guardado.\n")
	}

	name, err := readLineTrimOrDefault("Nombre visible [guest]: ", "guest")
	if err != nil {
		return 1
	}

	mode := "auto"
	relay := ""
	direct := false
	password := ""

	m := effectiveJoinMode(mode, code)
	if m == "relay" || m == "direct" {
		password, err = readOptionalPasswordLine()
		if err != nil {
			return 1
		}
	}

	return execRoomJoin(name, code, mode, relay, direct, password)
}

func loadAnyLastRoomCode() (string, error) {
	if c, err := p2pcmd.LoadLastCode(); err == nil && strings.TrimSpace(c) != "" {
		return strings.TrimSpace(c), nil
	}
	return chat.LoadLastRoomCode()
}

func isWildcardListen(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	return host == "0.0.0.0" || host == "::"
}
