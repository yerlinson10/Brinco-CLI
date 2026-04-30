package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"brinco-cli/internal/chat"
	p2pcmd "brinco-cli/internal/p2p"
)

func validateTCPHostPort(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return errors.New("dirección vacía")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("formato host:puerto inválido: %w", err)
	}
	if host == "" {
		return errors.New("falta el host (ej. 127.0.0.1:10000)")
	}
	if port == "" {
		return errors.New("falta el puerto")
	}
	return nil
}

func isAffirmative(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "s", "si", "sí", "y", "yes":
		return true
	default:
		return false
	}
}

// readRelayTCPHostPortAndPassword pide relay TCP (obligatorio), valida host:puerto y la contraseña opcional de sala.
func readRelayTCPHostPortAndPassword(prompt, def, emptyErrMsg string) (relay, password string, exitCode int) {
	var err error
	relay, err = readLineTrimOrDefault(prompt, def)
	if err != nil {
		return "", "", 1
	}
	if strings.TrimSpace(relay) == "" {
		fmt.Fprintln(os.Stderr, emptyErrMsg)
		return "", "", 1
	}
	if err := validateTCPHostPort(relay); err != nil {
		fmt.Fprintf(os.Stderr, "Error: relay: %v\n", err)
		return "", "", 1
	}
	password, err = readOptionalPasswordLine()
	if err != nil {
		return "", "", 1
	}
	return relay, password, 0
}

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
	case "guaranteed":
		fmt.Fprintln(os.Stderr, "Modo garantizado: no se solicitan relay ni contraseña en este asistente.")
	case "relay":
		var ec int
		relay, password, ec = readRelayTCPHostPortAndPassword(
			"Dirección del relay TCP host:puerto [127.0.0.1:10000]: ",
			"127.0.0.1:10000",
			"Error: relay obligatorio en modo relay",
		)
		if ec != 0 {
			return ec
		}
	case "direct":
		listen, err = readLineTrimOrDefault("Escucha local (listen) [0.0.0.0:9090]: ", "0.0.0.0:9090")
		if err != nil {
			return 1
		}
		if err := validateTCPHostPort(listen); err != nil {
			fmt.Fprintf(os.Stderr, "Error: escucha (listen): %v\n", err)
			return 1
		}
		public, err = readLineTrim("Host:puerto público (--public, obligatorio si escuchas en 0.0.0.0): ")
		if err != nil {
			return 1
		}
		if isWildcardListen(listen) && strings.TrimSpace(public) == "" {
			fmt.Fprintln(os.Stderr, "Error: con 0.0.0.0 debes indicar host:puerto público")
			return 1
		}
		if strings.TrimSpace(public) != "" {
			if err := validateTCPHostPort(public); err != nil {
				fmt.Fprintf(os.Stderr, "Error: público: %v\n", err)
				return 1
			}
		}
		useRelay, err := readLineTrimOrDefault("¿Crear sala sobre relay TCP en vez de servidor propio? (s/N) [N]: ", "n")
		if err != nil {
			return 1
		}
		if isAffirmative(useRelay) {
			var ec int
			relay, password, ec = readRelayTCPHostPortAndPassword(
				"Relay host:puerto [127.0.0.1:10000]: ",
				"127.0.0.1:10000",
				"Error: relay obligatorio si eliges relay",
			)
			if ec != 0 {
				return ec
			}
			mode = "direct"
			direct = true
		} else {
			password, err = readOptionalPasswordLine()
			if err != nil {
				return 1
			}
		}
	case "p2p":
		relay, err = readLineTrim("Relay libp2p multiaddr (opcional, Enter vacío): ")
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

	code, err := readLineTrim("Código de sala (Enter para usar el último guardado en disco): ")
	if err != nil {
		return 1
	}
	code = strings.TrimSpace(code)
	if code == "" {
		var loadErr error
		code, loadErr = loadAnyLastRoomCode()
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", loadErr)
			return 1
		}
		if strings.TrimSpace(code) == "" {
			fmt.Fprintln(os.Stderr, "Error: no hay código de sala guardado.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "Usando código guardado.")
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
