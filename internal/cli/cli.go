package cli

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"brinco-cli/internal/chat"
	"brinco-cli/internal/logx"
	"brinco-cli/internal/notify"
	p2pcmd "brinco-cli/internal/p2p"
	"brinco-cli/internal/roomproto"
	"brinco-cli/internal/updater"
)

const version = "1.2.1"

const mainHelpText = `Chat P2P / relay / TCP

ATAJOS (lo mas habitual)
  brinco host                   Asistente interactivo (sin flags)
  brinco host [flags]           Crear sala (equivale a: brinco room create)
  brinco join                   Asistente interactivo (sin argumentos)
  brinco join [CODIGO] [flags] Unirse (CODIGO posicional opcional si usas --code o perfil)
  brinco relay [--listen ...]   Levantar relay TCP (equivale a: relay serve; "serve" es opcional)
  brinco doctor                 Version, rutas y comprobaciones rapidas

COMANDOS CLASICOS
  brinco room create|join|code
  brinco relay serve ...
  brinco update check|apply
  brinco version

AYUDA DETALLADA
  brinco help
  brinco room help
  brinco relay help
  brinco update help

Perfiles locales: %%CONFIG%%/brinco-cli/profiles.json
  Ejemplo clave "casa": {"relay":"127.0.0.1:10000","mode":"relay","password":"demo","code":"relay-..."}
  Uso: brinco join @casa

CODIGOS: p2p- | guaranteed- | relay- | direct-
Logs: %%LOCALAPPDATA%%\brinco-cli\logs\brinco.log (Windows) o cache del sistema.
`

const roomHelpText = `brinco room — Salas (y alias brinco host / brinco join)

ATAJOS
  brinco host                   sin flags: asistente (nombre, modo, relay, password opcional)
  brinco host [mismos flags que room create]
  brinco join                   sin args: asistente (codigo, nombre, password opcional en TCP)
  brinco join CODIGO [flags]     codigo como primer argumento (sin --code)
  brinco join @perfil [flags]     carga profiles.json (relay, mode, password, code...)
  brinco room create            sin mas args: mismo asistente que brinco host
  brinco room join              sin mas args: mismo asistente que brinco join

room create / host
  --name       (default host)
  --mode       p2p | direct | relay | guaranteed (default p2p)
  --relay      p2p: multiaddr libp2p opcional | relay: host:puerto TCP obligatorio
  --password, --pass   clave opcional; vacio = sala abierta (sin clave) en relay/direct
  --notify-sound on|off, --notify-level all|direct|mentions|none
  --file-limit 10MB     limite local para /send
  --direct     atajo a modo direct
  --listen, --public   solo direct

room join / join
  CODIGO o --code   obligatorio salvo asistente (Enter reutiliza ultimo codigo guardado)
  --name, --mode, --relay, --password, --pass, --direct   como arriba
  Password opcional en relay/direct; vacio = sala sin clave

DENTRO DEL CHAT
  @usuario mensaje     privado (atajo de /msg)
  /msg usuario texto   privado
  /send ruta           archivo chunked con progreso/checksum
  /diag /peers /code /quit /help
  Reacciones en linea sola: +1  -1  ok  :emoji:
`

const relayHelpText = `brinco relay — Servidor TCP para modo relay

Formas equivalentes:
  brinco relay serve --listen 0.0.0.0:10000 --public IP:10000
  brinco relay --listen 0.0.0.0:10000 --public IP:10000

FLAGS
  --listen   local (default 0.0.0.0:10000)
  --public   host:puerto publico (obligatorio si escuchas en 0.0.0.0)
`

const updateHelpText = `brinco update — GitHub Releases (yerlinson10/Brinco-CLI)
  brinco update check | apply | help

  Variables de entorno (opcional):
    BRINCO_UPDATE_PRERELEASE=1     usar el release con tag semver más alto (incluye pre-releases)
    BRINCO_UPDATE_RELEASE_API=URL JSON de un solo release (sobrescribe la API por defecto)
    BRINCO_UPDATE_REQUIRE_CHECKSUM=1  falla si no hay checksums.txt o no coincide
    BRINCO_UPDATE_REQUIRE_SIGNATURE=1 valida firma minisign de checksums.txt
    BRINCO_UPDATE_MINISIGN_PUBKEY=RWT3gbCcNzwhERPbMCrksoJYot+hxt8y1Xz2C76I6vFweMl/puY6Wz7z
    BRINCO_UPDATE_PROGRESS=1      muestra progreso de descarga en la consola
    BRINCO_UPDATE_WINDOWS_DEFERRED=0  en Windows no programa reemplazo al salir (solo deja .exe.new)
    Linux/macOS: primero reemplazo atómico; si el binario está en uso, se programa el mismo reemplazo al salir que en Windows.
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
	case "doctor":
		return runDoctor(args[1:])
	case "host":
		return runHost(args[1:])
	case "join":
		return runJoinShortcut(args[1:])
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

func printMainHelp() {
	fmt.Printf("brinco-cli %s\n\n", version)
	s := mainHelpText
	if cfg, err := os.UserConfigDir(); err == nil {
		s = strings.ReplaceAll(s, "%%CONFIG%%", cfg)
	} else {
		s = strings.ReplaceAll(s, "%%CONFIG%%", "(config)")
	}
	fmt.Print(s)
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	verbose := fs.Bool("verbose", false, "Diagnostico extendido")
	relay := fs.String("relay", "", "Relay(s) host:puerto separados por coma para probar")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	fmt.Printf("brinco-cli %s\n", version)
	fmt.Printf("go %s  %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("cwd: %s\n", wd)
	}
	if d, err := os.UserCacheDir(); err == nil {
		fmt.Printf("cache: %s\n", filepath.Join(d, "brinco-cli"))
	}
	if d, err := profilesConfigDir(); err == nil {
		fmt.Printf("config: %s\n", d)
	}
	if p, err := profilesPath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			fmt.Printf("profiles: %s (existe)\n", p)
		} else {
			fmt.Printf("profiles: %s (aun no existe; opcional)\n", p)
		}
	}
	if !*verbose {
		return 0
	}
	fmt.Println()
	fmt.Println("Diagnostico verbose")
	if p := logx.Path(); p != "" {
		fmt.Printf("log: %s\n", p)
	}
	printDoctorNetwork()
	relays := chat.SplitRelayList(*relay)
	if len(relays) == 0 {
		relays = relaysFromProfiles()
	}
	if len(relays) == 0 {
		relays = []string{"127.0.0.1:10000"}
	}
	fmt.Println("relays:")
	for _, result := range chat.ProbeTCPRelays(relays, 1200*time.Millisecond) {
		if result.OK {
			fmt.Printf("- %s ok rtt=%s\n", result.Addr, result.RTT.Round(time.Millisecond))
		} else {
			fmt.Printf("- %s fail err=%s\n", result.Addr, result.Message)
		}
	}
	return 0
}

func printDoctorNetwork() {
	fmt.Println("interfaces:")
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("- error: %v\n", err)
		return
	}
	nat := "posible publico"
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			s := addr.String()
			if strings.Contains(s, "10.") || strings.Contains(s, "192.168.") || strings.Contains(s, "172.") || strings.Contains(s, "fd") {
				nat = "probablemente detras de NAT"
			}
			fmt.Printf("- %s %s\n", iface.Name, s)
		}
	}
	fmt.Printf("nat: %s\n", nat)
	fmt.Println("puertos comunes:")
	for _, addr := range []string{"127.0.0.1:9090", "127.0.0.1:10000"} {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			fmt.Printf("- %s cerrado/no alcanzable\n", addr)
			continue
		}
		_ = conn.Close()
		fmt.Printf("- %s abierto rtt=%s\n", addr, time.Since(start).Round(time.Millisecond))
	}
}

func relaysFromProfiles() []string {
	path, err := profilesPath()
	if err != nil {
		return nil
	}
	raw, err := readProfilesFile(path)
	if err != nil {
		return nil
	}
	all, err := parseProfilesBytes(raw)
	if err != nil {
		return nil
	}
	var relays []string
	for _, profile := range all {
		relays = append(relays, chat.SplitRelayList(profile.Relay)...)
	}
	return uniqueStrings(relays)
}

func mergePass(a, b string) string {
	b = strings.TrimSpace(b)
	if b != "" {
		return b
	}
	return strings.TrimSpace(a)
}

func effectiveJoinMode(mode, code string) string {
	if mode != "auto" {
		return mode
	}
	p, _ := roomproto.Unwrap(code)
	if p != "" {
		return p
	}
	return "p2p"
}

type runtimeOptions struct {
	NotifySound string
	NotifyLevel string
	FileLimit   string
}

func applyRuntimeOptions(opts runtimeOptions) {
	sound := true
	switch strings.ToLower(strings.TrimSpace(opts.NotifySound)) {
	case "off", "false", "0", "no":
		sound = false
	}
	level := strings.TrimSpace(opts.NotifyLevel)
	if level == "" {
		level = notify.LevelAll
	}
	notify.Configure(notify.Options{Sound: sound, Level: level, Throttle: 250 * time.Millisecond})
	if limit, err := parseByteLimit(opts.FileLimit); err == nil && limit > 0 {
		chat.SetMaxFileBytes(limit)
		p2pcmd.SetMaxFileBytes(limit)
	}
}

func parseByteLimit(value string) (int64, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return 0, nil
	}
	mult := int64(1)
	for _, suffix := range []struct {
		s string
		m int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"G", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"M", 1024 * 1024},
		{"KB", 1024},
		{"K", 1024},
	} {
		if strings.HasSuffix(value, suffix.s) {
			mult = suffix.m
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix.s))
			break
		}
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return int64(n * float64(mult)), nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func execRoomCreate(name, mode, relay string, direct bool, listen, public, password string) int {
	if direct {
		mode = "direct"
	}
	switch mode {
	case "direct":
		if relay != "" {
			return chat.RunCreateUsingRelay(name, relay, password)
		}
		return chat.RunCreate(name, listen, public, password)
	case "relay":
		return chat.RunCreateUsingRelayWithProtocol(name, relay, password, roomproto.ProtocolRelay)
	case "guaranteed":
		return p2pcmd.RunCreateGuaranteed(name)
	case "p2p":
		return p2pcmd.RunCreate(name, relay)
	default:
		fmt.Fprintf(os.Stderr, "Modo no soportado: %s\n", mode)
		return 1
	}
}

func execRoomJoin(name, code, mode, relay string, direct bool, password string) int {
	if direct {
		mode = "direct"
	}
	if mode == "auto" {
		p, _ := roomproto.Unwrap(code)
		if p != "" {
			mode = p
		} else {
			mode = "p2p"
		}
	}
	switch mode {
	case "p2p":
		return p2pcmd.RunJoin(name, code, relay)
	case "direct", "relay":
		return chat.RunJoin(name, code, password)
	case "guaranteed":
		return p2pcmd.RunJoinGuaranteed(name, code)
	default:
		fmt.Fprintf(os.Stderr, "Modo no soportado: %s\n", mode)
		return 1
	}
}

func runHost(args []string) int {
	if len(args) == 0 {
		return runHostGuided()
	}
	fs := flag.NewFlagSet("host", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "host", "Nombre de usuario")
	mode := fs.String("mode", "p2p", "Modo: p2p | direct | relay | guaranteed")
	relay := fs.String("relay", "", "Relay segun modo")
	direct := fs.Bool("direct", false, "Modo TCP directo")
	listen := fs.String("listen", "0.0.0.0:9090", "[direct] Escucha local")
	public := fs.String("public", "", "[direct] Host:puerto publico")
	password := fs.String("password", "", "Password de la sala")
	pass := fs.String("pass", "", "Alias de --password")
	notifySound := fs.String("notify-sound", "on", "Sonido: on | off")
	notifyLevel := fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none")
	fileLimit := fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *direct {
		*mode = "direct"
	}
	pw := mergePass(*password, *pass)
	applyRuntimeOptions(runtimeOptions{NotifySound: *notifySound, NotifyLevel: *notifyLevel, FileLimit: *fileLimit})
	return execRoomCreate(*name, *mode, *relay, *direct, *listen, *public, pw)
}

func runJoinShortcut(args []string) int {
	if len(args) == 0 {
		return runJoinGuided()
	}
	prof := Profile{}
	rest := args
	if len(rest) > 0 && strings.HasPrefix(rest[0], "@") {
		pn := strings.TrimPrefix(rest[0], "@")
		if !looksLikeRoomCode(rest[0]) {
			var err error
			prof, err = loadProfile(pn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				return 1
			}
			rest = rest[1:]
		}
	}

	code := strings.TrimSpace(prof.Code)
	if len(rest) > 0 && looksLikeRoomCode(rest[0]) {
		code = rest[0]
		rest = rest[1:]
	}

	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "guest", "Nombre")
	codeFlag := fs.String("code", "", "Codigo de sala")
	mode := fs.String("mode", "auto", "Modo")
	relay := fs.String("relay", "", "Relay libp2p (p2p)")
	direct := fs.Bool("direct", false, "Modo TCP directo")
	password := fs.String("password", "", "Password")
	pass := fs.String("pass", "", "Alias de --password")
	notifySound := fs.String("notify-sound", "on", "Sonido: on | off")
	notifyLevel := fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none")
	fileLimit := fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)")
	if err := fs.Parse(rest); err != nil {
		return 1
	}
	if *direct {
		*mode = "direct"
	}
	if strings.TrimSpace(*codeFlag) != "" {
		code = *codeFlag
	}
	if strings.TrimSpace(prof.Name) != "" && *name == "guest" {
		*name = prof.Name
	}
	if prof.Mode != "" && *mode == "auto" {
		*mode = prof.Mode
	}
	relayVal := *relay
	if strings.TrimSpace(prof.Relay) != "" && relayVal == "" {
		relayVal = prof.Relay
	}
	pw := mergePass(*password, *pass)
	if strings.TrimSpace(prof.Password) != "" && pw == "" {
		pw = prof.Password
	}
	if strings.TrimSpace(prof.NotifySound) != "" && *notifySound == "on" {
		*notifySound = prof.NotifySound
	}
	if strings.TrimSpace(prof.NotifyLevel) != "" && *notifyLevel == "all" {
		*notifyLevel = prof.NotifyLevel
	}
	if strings.TrimSpace(prof.FileLimit) != "" && *fileLimit == "10MB" {
		*fileLimit = prof.FileLimit
	}

	if strings.TrimSpace(code) == "" {
		var err error
		code, err = readLineTrim("Codigo de sala: ")
		if err != nil || strings.TrimSpace(code) == "" {
			fmt.Fprintln(os.Stderr, "Error: codigo de sala obligatorio")
			return 1
		}
	}

	applyRuntimeOptions(runtimeOptions{NotifySound: *notifySound, NotifyLevel: *notifyLevel, FileLimit: *fileLimit})
	return execRoomJoin(*name, code, *mode, relayVal, *direct, pw)
}

func runRoom(args []string) int {
	if len(args) == 0 {
		printRoomHelp()
		return 0
	}

	switch args[0] {
	case "create":
		if len(args) == 1 {
			return runHostGuided()
		}
		fs := flag.NewFlagSet("room create", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "host", "Nombre de usuario")
		mode := fs.String("mode", "p2p", "Modo: p2p | direct | relay | guaranteed")
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo (requiere IP publica)")
		listen := fs.String("listen", "0.0.0.0:9090", "[--direct] Direccion local")
		public := fs.String("public", "", "[--direct] Direccion publica host:puerto")
		password := fs.String("password", "", "Password de la sala")
		pass := fs.String("pass", "", "Alias de --password")
		notifySound := fs.String("notify-sound", "on", "Sonido: on | off")
		notifyLevel := fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none")
		fileLimit := fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			*mode = "direct"
		}
		pw := mergePass(*password, *pass)
		applyRuntimeOptions(runtimeOptions{NotifySound: *notifySound, NotifyLevel: *notifyLevel, FileLimit: *fileLimit})
		return execRoomCreate(*name, *mode, *relay, *direct, *listen, *public, pw)

	case "join":
		if len(args) == 1 {
			return runJoinGuided()
		}
		fs := flag.NewFlagSet("room join", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "guest", "Nombre de usuario")
		code := fs.String("code", "", "Codigo de la sala")
		mode := fs.String("mode", "auto", "Modo: auto | p2p | direct | relay | guaranteed")
		relay := fs.String("relay", "", "Multiaddr relay propio (opcional)")
		direct := fs.Bool("direct", false, "Modo TCP directo")
		password := fs.String("password", "", "Password de la sala")
		pass := fs.String("pass", "", "Alias de --password")
		notifySound := fs.String("notify-sound", "on", "Sonido: on | off")
		notifyLevel := fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none")
		fileLimit := fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *direct {
			*mode = "direct"
		}
		c := strings.TrimSpace(*code)
		if c == "" {
			var err error
			c, err = readLineTrim("Codigo de sala: ")
			if err != nil || strings.TrimSpace(c) == "" {
				fmt.Fprintln(os.Stderr, "Error: codigo de sala obligatorio")
				return 1
			}
		}
		pw := mergePass(*password, *pass)
		applyRuntimeOptions(runtimeOptions{NotifySound: *notifySound, NotifyLevel: *notifyLevel, FileLimit: *fileLimit})
		return execRoomJoin(*name, c, *mode, *relay, *direct, pw)

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
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printRelayHelp()
		return 0
	}
	serveArgs := args
	if args[0] == "serve" {
		serveArgs = args[1:]
	}
	fs := flag.NewFlagSet("relay serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", "0.0.0.0:10000", "Direccion local del relay host:puerto")
	public := fs.String("public", "", "Direccion publica del relay host:puerto")
	if err := fs.Parse(serveArgs); err != nil {
		return 1
	}
	return chat.RunRelayServer(*listen, *public)
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
