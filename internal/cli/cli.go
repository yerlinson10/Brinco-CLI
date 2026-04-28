package cli

import (
	"flag"
	"fmt"
	"os"

	"brinco-cli/internal/chat"
)

const version = "0.1.0"

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
	case "room":
		return runRoom(args[1:])
	case "hello":
		name := "mundo"
		if len(args) > 1 {
			name = args[1]
		}
		fmt.Printf("Hola, %s!\n", name)
		return 0
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
		listen := fs.String("listen", "0.0.0.0:9090", "Direccion local host:puerto")
		public := fs.String("public", "", "Direccion publica host:puerto para generar codigo")
		password := fs.String("password", "", "Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		return chat.RunCreate(*name, *listen, *public, *password)

	case "join":
		fs := flag.NewFlagSet("room join", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "guest", "Nombre de usuario")
		code := fs.String("code", "", "Codigo de la sala")
		password := fs.String("password", "", "Password de la sala")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		return chat.RunJoin(*name, *code, *password)

	case "code":
		code, err := chat.LoadLastRoomCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
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

func printHelp() {
	fmt.Println(`brinco-cli - CLI con chat P2P por salas

Uso:
  brinco <comando> [argumentos]

Comandos:
  room create       Crea una sala de chat
  room join         Se une a una sala por codigo
  room code         Muestra ultimo codigo de sala creado
  room help         Ayuda de subcomandos room
  version           Muestra la version
  help              Muestra esta ayuda

Ejemplos:
  brinco room create --name Ana --listen 0.0.0.0:9090 --public 203.0.113.10:9090 --password secreto
  brinco room code
  brinco room join --name Luis --code <codigo> --password secreto
  brinco version`)
}

func printRoomHelp() {
	fmt.Println(`Uso:
  brinco room <subcomando> [flags]

Subcomandos:
  create    Crea una sala y entra al chat
  join      Se une a una sala usando codigo
  code      Muestra el ultimo codigo de sala guardado
  help      Muestra esta ayuda

Create flags:
  --name       Nombre visible en la sala (default: host)
  --listen     Direccion local host:puerto (default: 0.0.0.0:9090)
  --public     Direccion publica host:puerto para codigo
  --password   Password de la sala (obligatorio)

Join flags:
  --name       Nombre visible en la sala (default: guest)
  --code       Codigo de la sala (obligatorio)
  --password   Password de la sala (obligatorio)

Dentro del chat:
  /code  /peers  /quit  /help`)
}
