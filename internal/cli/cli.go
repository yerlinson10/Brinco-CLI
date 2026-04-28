package cli

import (
	"flag"
	"fmt"
	"os"

	"brinco-cli/internal/chat"
	"brinco-cli/internal/logx"
	p2pcmd "brinco-cli/internal/p2p"
	"brinco-cli/internal/roomproto"
)

const version = "0.2.0"

func Run(args []string) int {
	logx.Init()
	if len(args) == 0 {
		printHelp()
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp()
		return 0
	case "version", "-v", "--version":
		fmt.Printf("brinco-cli %s\n", version)
		return 0
	case "relay":
		return runRelay(args[1:])
	case "room":
		return runRoom(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Comando no reconocido: %s\n\n", args[0])
		printHelp()
		return 1
	}
}

func runRoom(args []string) int {
	if len(args) == 0 {
		printRoomHelp()
		return 0
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("room create", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "host", "Nombre de usuario")
		mode := fs.String("mode", "p2p", "Modo: p2p | direct | relay | guaranteed")
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo (requiere IP publica)")
		listen := fs.String("listen", "0.0.0.0:9090", "[--direct] Direccion local")
		public := fs.String("public", "", "[--direct] Direccion publica host:puerto")
		password := fs.String("password", "", "[direct|relay|guaranteed] Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			*mode = "direct"
		}

		switch *mode {
		case "direct":
			if *relay != "" {
				return chat.RunCreateUsingRelay(*name, *relay, *password)
			}
			return chat.RunCreate(*name, *listen, *public, *password)
		case "relay":
			return chat.RunCreateUsingRelayWithProtocol(*name, *relay, *password, roomproto.ProtocolRelay)
		case "guaranteed":
			return p2pcmd.RunCreateGuaranteed(*name)
		case "p2p":
			return p2pcmd.RunCreate(*name, *relay)
		default:
			fmt.Fprintf(os.Stderr, "Modo no soportado: %s\n", *mode)
			return 1
		}

	case "join":
		fs := flag.NewFlagSet("room join", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "guest", "Nombre de usuario")
		code := fs.String("code", "", "Codigo de la sala")
		mode := fs.String("mode", "auto", "Modo: auto | p2p | direct | relay | guaranteed")
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo")
		password := fs.String("password", "", "[direct|relay|guaranteed] Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			*mode = "direct"
		}

		if *mode == "auto" {
			p, _ := roomproto.Unwrap(*code)
			if p != "" {
				*mode = p
			} else {
				*mode = "p2p"
			}
		}

		switch *mode {
		case "p2p":
			return p2pcmd.RunJoin(*name, *code, *relay)
		case "direct", "relay":
			return chat.RunJoin(*name, *code, *password)
		case "guaranteed":
			return p2pcmd.RunJoinGuaranteed(*name, *code)
		default:
			fmt.Fprintf(os.Stderr, "Modo no soportado: %s\n", *mode)
			return 1
		}

	case "code":
		code, err := p2pcmd.LoadLastCode()
		if err != nil {
			code, err = chat.LoadLastRoomCode()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: no hay codigo guardado\n")
				return 1
			}
		}
		fmt.Println(code)
		return 0

	case "help", "-h", "--help":
		printRoomHelp()
		return 0

	default:
		fmt.Fprintf(os.Stderr, "Subcomando room no reconocido: %s\n\n", args[0])
		printRoomHelp()
		return 1
	}
}

func runRelay(args []string) int {
	if len(args) == 0 {
		printRelayHelp()
		return 0
	}

	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("relay serve", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		listen := fs.String("listen", "0.0.0.0:10000", "Direccion local del relay host:puerto")
		public := fs.String("public", "", "Direccion publica del relay host:puerto")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		return chat.RunRelayServer(*listen, *public)
	case "help", "-h", "--help":
		printRelayHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Subcomando relay no reconocido: %s\n\n", args[0])
		printRelayHelp()
		return 1
	}
}

func printHelp() {
	fmt.Println("brinco-cli v" + version + " - Chat P2P/Relay\n\nUso:\n  brinco <comando> [argumentos]\n\nComandos:\n  room create       Crea sala\n  room join         Se une a sala por codigo\n  room code         Muestra ultimo codigo de sala\n  relay serve       Levanta relay TCP propio\n  version           Muestra la version\n  help              Muestra esta ayuda\n\nCodigos de sala:\n  p2p-<...> direct-<...> relay-<...> guaranteed-<...>\n\nLogs:\n  Archivo en cache de usuario: brinco-cli/logs/brinco.log")
}

func printRoomHelp() {
	fmt.Println("Uso:\n  brinco room <subcomando> [flags]\n\nSubcomandos:\n  create    Crea sala y entra al chat\n  join      Se une a sala usando codigo\n  code      Muestra el ultimo codigo guardado\n  help      Muestra esta ayuda\n\ncreate flags:\n  --name       Nombre visible (default: host)\n  --mode       p2p | direct | relay | guaranteed (default: p2p)\n  --relay      Relay propio\n  --password   Requerido en direct/relay/guaranteed\n  --direct     Atajo legacy para mode=direct\n\njoin flags:\n  --name       Nombre visible (default: guest)\n  --code       Codigo de sala (obligatorio)\n  --mode       auto | p2p | direct | relay | guaranteed (default: auto)\n  --relay      Relay para p2p\n  --password   Requerido en direct/relay/guaranteed\n  --direct     Atajo legacy para mode=direct\n\nDentro del chat:\n  /code  /peers  /quit  /help")
}

func printRelayHelp() {
	fmt.Println("Uso:\n  brinco relay serve [flags]\n\nFlags:\n  --listen   Direccion local (default: 0.0.0.0:10000)\n  --public   Direccion publica host:puerto\n\nNota: el relay propio es OPCIONAL.\nSin el, brinco usa la red publica libp2p/IPFS gratuitamente.")
}
