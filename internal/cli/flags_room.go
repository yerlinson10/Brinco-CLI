package cli

import (
	"flag"
	"fmt"
	"os"
)

// roomCreateFlags holds pointers registered on a FlagSet for host / room create.
type roomCreateFlags struct {
	Name, Mode, Relay, Listen, Public, Password, Pass *string
	Direct                                             *bool
	NotifySound, NotifyLevel, FileLimit                *string
	NoTUI                                              *bool
}

type roomCreateFlagDoc struct {
	Relay, Listen, Public, Direct string
}

func docRoomCreateHost() roomCreateFlagDoc {
	return roomCreateFlagDoc{
		Relay:  "Relay segun modo",
		Listen: "[direct] Escucha local",
		Public: "[direct] Host:puerto publico",
		Direct: "Modo TCP directo",
	}
}

func docRoomCreateCmd() roomCreateFlagDoc {
	return roomCreateFlagDoc{
		Relay:  "Multiaddr relay propio (opcional)",
		Listen: "[--direct] Direccion local",
		Public: "[--direct] Direccion publica host:puerto",
		Direct: "Modo TCP directo (requiere IP publica)",
	}
}

func registerRoomCreateFlags(fs *flag.FlagSet, doc roomCreateFlagDoc) roomCreateFlags {
	return roomCreateFlags{
		Name:        fs.String("name", "host", "Nombre de usuario"),
		Mode:        fs.String("mode", "p2p", "Modo: p2p | direct | relay | guaranteed"),
		Relay:       fs.String("relay", "", doc.Relay),
		Direct:      fs.Bool("direct", false, doc.Direct),
		Listen:      fs.String("listen", "0.0.0.0:9090", doc.Listen),
		Public:      fs.String("public", "", doc.Public),
		Password:    fs.String("password", "", "Password de la sala"),
		Pass:        fs.String("pass", "", "Alias de --password"),
		NotifySound: fs.String("notify-sound", "on", "Sonido: on | off"),
		NotifyLevel: fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none"),
		FileLimit:   fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)"),
		NoTUI:       fs.Bool("no-tui", false, "Sin TUI: modo linea clasico (BRINCO_TUI=0)"),
	}
}

type roomJoinFlags struct {
	Name, Code, Mode, Relay, Password, Pass *string
	Direct                                   *bool
	NotifySound, NotifyLevel, FileLimit      *string
	NoTUI                                    *bool
}

type roomJoinFlagDoc struct {
	Name, Mode, Relay, Password, Code string
}

func docJoinShortcut() roomJoinFlagDoc {
	return roomJoinFlagDoc{
		Name:     "Nombre",
		Mode:     "Modo",
		Relay:    "Relay libp2p (p2p)",
		Password: "Password",
		Code:     "Codigo de sala",
	}
}

func docRoomJoinCmd() roomJoinFlagDoc {
	return roomJoinFlagDoc{
		Name:     "Nombre de usuario",
		Mode:     "Modo: auto | p2p | direct | relay | guaranteed",
		Relay:    "Multiaddr relay propio (opcional)",
		Password: "Password de la sala",
		Code:     "Codigo de la sala",
	}
}

func registerRoomJoinFlags(fs *flag.FlagSet, doc roomJoinFlagDoc) roomJoinFlags {
	return roomJoinFlags{
		Name:        fs.String("name", "guest", doc.Name),
		Code:        fs.String("code", "", doc.Code),
		Mode:        fs.String("mode", "auto", doc.Mode),
		Relay:       fs.String("relay", "", doc.Relay),
		Direct:      fs.Bool("direct", false, "Modo TCP directo"),
		Password:    fs.String("password", "", doc.Password),
		Pass:        fs.String("pass", "", "Alias de --password"),
		NotifySound: fs.String("notify-sound", "on", "Sonido: on | off"),
		NotifyLevel: fs.String("notify-level", "all", "Notificaciones: all | direct | mentions | none"),
		FileLimit:   fs.String("file-limit", "10MB", "Limite para /send (ej. 10MB)"),
		NoTUI:       fs.Bool("no-tui", false, "Sin TUI: modo linea clasico (BRINCO_TUI=0)"),
	}
}

// checkFlagParse validates FlagSet.Parse result and rejects leftover positional args.
func checkFlagParse(fs *flag.FlagSet, err error, cmd string) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error en flags (%s): %v\n", cmd, err)
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Argumento no reconocido en %s: %q\n", cmd, fs.Args()[0])
		return 1
	}
	return 0
}
