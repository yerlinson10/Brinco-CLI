package cli

import (
	"flag"
	"fmt"
	"os"

	"brinco-cli/internal/chat"
	p2pcmd "brinco-cli/internal/p2p"
)

const version = "0.2.0"

func Run(args []string) int {
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
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo (requiere IP publica)")
		listen := fs.String("listen", "0.0.0.0:9090", "[--direct] Direccion local")
		public := fs.String("public", "", "[--direct] Direccion publica host:puerto")
		password := fs.String("password", "", "[--direct] Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			if *relay != "" {
				return chat.RunCreateUsingRelay(*name, *relay, *password)
			}
			return chat.RunCreate(*name, *listen, *public, *password)
		}
		return p2pcmd.RunCreate(*name, *relay)

	case "join":
		fs := flag.NewFlagSet("room join", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "guest", "Nombre de usuario")
		code := fs.String("code", "", "Codigo de la sala")
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo")
		password := fs.String("password", "", "[--direct] Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			return chat.RunJoin(*name, *code, *password)
		}
		return p2pcmd.RunJoin(*name, *code, *relay)

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
	fmt.Println("brinco-cli v" + version + " - Chat P2P descentralizado\n\nUso:\n  brinco <comando> [argumentos]\n\nComandos:\n  room create       Crea sala (libp2p, gratis, sin servidor)\n  room join         Se une a sala por codigo\n  room code         Muestra ultimo codigo de sala\n  relay serve       Levanta relay TCP propio (opcional)\n  version           Muestra la version\n  help              Muestra esta ayuda\n\nInicio rapido (sin servidor, gratis):\n  brinco room create --name Ana\n  brinco room join --name Luis --code <codigo>\n\nCon relay propio (opcional):\n  brinco relay serve --listen 0.0.0.0:10000 --public <IP>:10000\n  brinco room create --name Ana --relay /ip4/<IP>/tcp/10000/p2p/<PeerID>")
}

func printRoomHelp() {
	fmt.Println("Uso:\n  brinco room <subcomando> [flags]\n\nSubcomandos:\n  create    Crea sala y entra al chat (libp2p por defecto)\n  join      Se une a sala usando codigo\n  code      Muestra el ultimo codigo guardado\n  help      Muestra esta ayuda\n\ncreate flags:\n  --name     Nombre visible (default: host)\n  --relay    Multiaddr relay propio (opcional)\n  --direct   Modo TCP directo (requiere IP publica)\n\njoin flags:\n  --name     Nombre visible (default: guest)\n  --code     Codigo de sala (obligatorio)\n  --relay    Multiaddr relay propio (opcional)\n  --direct   Modo TCP directo\n\nDentro del chat:\n  /code  /quit  /help")
}

func printRelayHelp() {
	fmt.Println("Uso:\n  brinco relay serve [flags]\n\nFlags:\n  --listen   Direccion local (default: 0.0.0.0:10000)\n  --public   Direccion publica host:puerto\n\nNota: el relay propio es OPCIONAL.\nSin el, brinco usa la red publica libp2p/IPFS gratuitamente.")
}
