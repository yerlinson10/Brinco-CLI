package cli

import (
	"flag"
	"fmt"
	"os"

	"brinco-cli/internal/chat"
	"brinco-cli/internal/logx"
	p2pcmd "brinco-cli/internal/p2p"
	"brinco-cli/internal/roomproto"
	"brinco-cli/internal/updater"
)

const version = "0.2.0"

const mainHelpText = `brinco-cli v%s - Chat P2P / relay / TCP desde consola

USO GENERAL
  brinco <comando> [argumentos]
  brinco help              Esta ayuda
  brinco version           Version del binario

COMANDOS PRINCIPALES
  room create|join|code    Salas de chat (ver: brinco room help)
  relay serve              Relay TCP propio para modo relay (ver: brinco relay help)
  update check|apply       Actualizacion desde GitHub Releases (ver: brinco update help)

CODIGOS DE SALA (prefijo = protocolo)
  p2p-...         libp2p + GossipSub (relay libp2p opcional con --relay)
  guaranteed-...  libp2p con relay automatico via red IPFS
  relay-...       Cliente TCP hacia tu relay (brinco relay serve)
  direct-...      TCP directo entre host y clientes

LOGS
  Archivo bajo el directorio de cache del usuario, p. ej. Windows:
  %%LOCALAPPDATA%%\brinco-cli\logs\brinco.log
  Nivel: variable de entorno BRINCO_LOG_LEVEL (debug|info|warn|error)

Mas detalle: brinco room help | brinco relay help | brinco update help
`

const roomHelpText = `brinco room — Salas de chat

USO
  brinco room create [flags]
  brinco room join   [flags]
  brinco room code

SUBCOMANDOS
  create   Crea sala y entra al chat interactivo
  join     Entra con --code <codigo>
  code     Imprime el ultimo codigo guardado en disco (p2p o chat TCP)

--- room create ---

FLAGS
  --name string       Nombre visible inicial (default: host). El servidor puede asignar
                      un sufijo (#2) si el nombre ya existe (modo relay/direct).
  --mode string       p2p | direct | relay | guaranteed (default: p2p)
  --relay string      p2p: multiaddr relay libp2p opcional, ej. /ip4/HOST/tcp/PORT/p2p/PEERID
                      relay: host:puerto del relay TCP (obligatorio en --mode relay)
  --password string   Obligatorio en direct y relay. No se usa en create p2p/guaranteed.
  --direct            Atajo: equivale a --mode direct
  --listen string     Solo direct: escucha local (default: 0.0.0.0:9090)
  --public string     Solo direct: host:puerto que veran los demas (obligatorio si listen es 0.0.0.0)

NOTAS
  Modo direct con --relay: crea sala usando relay TCP (caso avanzado).
  Modo guaranteed: sin relay manual; espera direccion relay en la red IPFS.

--- room join ---

FLAGS
  --name string       Nombre visible (default: guest)
  --code string       Codigo de sala (obligatorio)
  --mode string       auto | p2p | direct | relay | guaranteed (default: auto)
                      auto: el prefijo del codigo fija el modo (p2p-, relay-, etc.)
  --relay string      Solo p2p: relay libp2p extra opcional
  --password string   Obligatorio en direct y relay (misma clave que el host)
  --direct            Atajo: --mode direct

--- Dentro del chat (todos los modos salvo detalles solo p2p) ---

  /code              Muestra el codigo de la sala
  /peers             Lista peers (TCP) o cuenta en topic (p2p)
  /diag              Diagnostico: estado, relay, NAT estimado; en p2p incluye RTT por peer
  /msg USER TEXTO    Mensaje privado solo para USER (nombre exacto en sala, ver "Tu nombre:")
  /send RUTA         Archivo pequeno (limite ~1.5 MB). El receptor lo guarda en Descargas
  /quit              Salir
  /help              Esta lista

  Reacciones (linea sola, sin /): +1  -1  ok  :emoji:
  Cualquier otra linea se envia como mensaje de chat.

SEGURIDAD / RED (resumen)
  relay/direct: anti-flood por conexion en el servidor; privados solo remitente y destinatario.
  p2p: mensajes de aplicacion cifrados con clave derivada del codigo de sala; huella TOFU por nick;
       rate limit por remitente en recepcion.
`

const relayHelpText = `brinco relay — Relay TCP propio (modo room relay)

USO
  brinco relay serve [flags]

FLAGS
  --listen string    Direccion local a escuchar (default: 0.0.0.0:10000)
  --public string    host:puerto publico que recibiran los clientes (obligatorio si listen es 0.0.0.0)

FLUJO TIPICO
  1) En la maquina publica: brinco relay serve --listen 0.0.0.0:10000 --public TU_IP:10000
  2) Creador: brinco room create --mode relay --relay TU_IP:10000 --password CLAVE --name host
  3) Invitados: brinco room join --code relay-... --password CLAVE --name invitado

El relay propio es opcional para p2p/guaranteed (ahi se usa la red libp2p/IPFS).
`

const updateHelpText = `brinco update — Actualizador (GitHub Releases: yerlinson10/Brinco-CLI)

USO
  brinco update check     Compara version local con la ultima release en GitHub
  brinco update apply     Descarga el zip/tar.gz para tu GOOS/GOARCH, verifica checksums.txt
                          si esta publicado, y reemplaza el binario (Unix) o deja .exe.new (Windows)

REQUISITOS
  Salida a Internet para api.github.com y descarga de assets.

NOTA
  La version embebida en el CLI es la constante interna; alinear tags de release con releases reales.
`

func Run(args []string) int {
	logx.Init()
	if len(args) == 0 {
		printMainHelp()
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printMainHelp()
		return 0
	case "version", "-v", "--version":
		fmt.Printf("brinco-cli %s\n", version)
		return 0
	case "relay":
		return runRelay(args[1:])
	case "update":
		return runUpdate(args[1:])
	case "room":
		return runRoom(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Comando no reconocido: %s\n\n", args[0])
		printMainHelp()
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

func printMainHelp() {
	fmt.Printf(mainHelpText, version)
}

func printRoomHelp() {
	fmt.Print(roomHelpText)
}

func printRelayHelp() {
	fmt.Print(relayHelpText)
}

func printUpdateHelp() {
	fmt.Print(updateHelpText)
}

func runUpdate(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUpdateHelp()
		return 0
	}
	if args[0] == "check" {
		latest, available, err := updater.Check(version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error verificando updates: %v\n", err)
			return 1
		}
		if !available {
			fmt.Println("Sin actualizaciones disponibles.")
			return 0
		}
		fmt.Printf("Nueva version disponible: %s (actual %s)\n", latest, version)
		return 0
	}
	if args[0] == "apply" {
		if err := updater.Apply(version); err != nil {
			fmt.Fprintf(os.Stderr, "Error aplicando update: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "Subcomando update no reconocido: %s (usa: brinco update help)\n", args[0])
	return 1
}
