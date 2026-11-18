package store

import "time"

// Session types.
const (
	TypeSSH    = "ssh"
	TypeSerial = "serial"
	TypeTelnet = "telnet"
	TypeLocal  = "local"
	TypeTCP    = "tcp"
	TypeUDP    = "udp"
)

// Auth methods for SSH.
const (
	AuthPassword = "password"
	AuthKey      = "key"
	AuthAgent    = "agent"
	AuthAuto     = "auto" // agent, then default keys, then password prompt
)

// Session is one saved connection. Secrets are never stored here; they live
// in the OS keychain keyed by session ID (see package secret).
type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Folder    string `json:"folder"` // "/"-separated path, e.g. "Site A/Rack 1"
	Type      string `json:"type"`
	Notes     string `json:"notes,omitempty"`
	Color     string `json:"color,omitempty"`
	Charset   string `json:"charset,omitempty"` // terminal charset: "" / "UTF-8" / "Big5"
	LogToFile bool   `json:"logToFile,omitempty"`

	SSH    *SSHConfig    `json:"ssh,omitempty"`
	Serial *SerialConfig `json:"serial,omitempty"`
	Telnet *TelnetConfig `json:"telnet,omitempty"`
	Local  *LocalConfig  `json:"local,omitempty"`
	Net    *NetConfig    `json:"net,omitempty"` // TCP / UDP terminal

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// JumpHost is one hop in a ProxyJump chain. Either SessionID references
// another saved SSH session (its auth is reused), or Host/Port/User are inline.
type JumpHost struct {
	SessionID string `json:"sessionId,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user,omitempty"`
}

// Tunnel is an SSH port forward definition.
type Tunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Kind       string `json:"kind"` // "L" local, "R" remote, "D" dynamic (SOCKS5)
	BindHost   string `json:"bindHost"`
	BindPort   int    `json:"bindPort"`
	TargetHost string `json:"targetHost,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	AutoStart  bool   `json:"autoStart,omitempty"`
}

type SSHConfig struct {
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	User           string     `json:"user"`
	Auth           string     `json:"auth"` // password | key | agent | auto
	KeyPath        string     `json:"keyPath,omitempty"`
	JumpHosts      []JumpHost `json:"jumpHosts,omitempty"`
	KeepAlive      int        `json:"keepAlive,omitempty"` // seconds, 0 = default 30
	AutoReconnect  bool       `json:"autoReconnect,omitempty"`
	StartupCommand string     `json:"startupCommand,omitempty"`
	TermType       string     `json:"termType,omitempty"` // default xterm-256color
	SFTPAutoOpen   bool       `json:"sftpAutoOpen"`
	Tunnels        []Tunnel   `json:"tunnels,omitempty"`
}

type SerialConfig struct {
	Port        string `json:"port"`
	Baud        int    `json:"baud"`
	DataBits    int    `json:"dataBits"`
	Parity      string `json:"parity"`        // none | odd | even | mark | space
	StopBits    string `json:"stopBits"`      // "1" | "1.5" | "2"
	DTR         *bool  `json:"dtr,omitempty"` // initial DTR (default on)
	RTS         *bool  `json:"rts,omitempty"` // initial RTS (default on)
	LocalEcho   bool   `json:"localEcho,omitempty"`
	SendLineEnd string `json:"sendLineEnd,omitempty"` // cr | lf | crlf (Enter key mapping)
}

type TelnetConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type LocalConfig struct {
	Shell string   `json:"shell,omitempty"`
	Args  []string `json:"args,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
}

// NetConfig is a raw TCP/UDP terminal (client or server).
type NetConfig struct {
	Mode       string `json:"mode"` // client | server
	Host       string `json:"host"` // remote host (client) or bind address (server)
	Port       int    `json:"port"`
	Format     string `json:"format,omitempty"` // text | json | hex (UI send/display hint)
	LocalEcho  bool   `json:"localEcho,omitempty"`
	AppendLine string `json:"appendLine,omitempty"` // none | lf | crlf, appended on send
}

// Settings are global preferences.
type Settings struct {
	Theme            string `json:"theme"`    // system | dark | light
	Language         string `json:"language"` // system | zh-TW | en (UI text is translated in the frontend)
	TerminalFont     string `json:"terminalFont"` // CSS font-family; empty = platform default (chosen in the frontend)
	TerminalFontSize int    `json:"terminalFontSize"`
	Scrollback       int    `json:"scrollback"`
	CopyOnSelect     bool   `json:"copyOnSelect"`
	// Ctrl+C copies when text is selected (otherwise sends ^C); Ctrl+V pastes.
	SmartCopyPaste bool `json:"smartCopyPaste"`
	// Right click in the terminal: "menu" (context menu) or "paste" (PuTTY style:
	// copies the selection if any, otherwise pastes).
	TermRightClick string `json:"termRightClick"`
	CursorBlink    bool   `json:"cursorBlink"`

	EditorFont        string `json:"editorFont"` // CSS font-family; empty = platform default
	EditorFontSize    int    `json:"editorFontSize"`
	EditorTabSize     int    `json:"editorTabSize"`
	EditorMinimap     bool   `json:"editorMinimap"`
	EditorWordWrap    bool   `json:"editorWordWrap"`
	BigFileLimitMB    int    `json:"bigFileLimitMB"`
	UseExternalEditor bool   `json:"useExternalEditor"`
	ExternalEditorCmd string `json:"externalEditorCmd"` // e.g. `code --wait {file}`; empty = auto-detect VS Code

	LogDir        string `json:"logDir"`
	LogStripANSI  bool   `json:"logStripAnsi"`
	LogTimestamps bool   `json:"logTimestamps"`

	SFTPFollowTerminal bool `json:"sftpFollowTerminal"` // SFTP panel follows the shell's cwd
	SFTPAutoSwitch     bool `json:"sftpAutoSwitch"`     // sidebar shows SFTP for SSH tabs, Sessions otherwise
	// After SSH login run `bind 'set completion-ignore-case on'` (bash) so
	// Tab completion ignores case. Off by default: non-bash CLIs would print an error.
	SSHCompletionIgnoreCase bool `json:"sshCompletionIgnoreCase"`

	ServerRoot        string `json:"serverRoot"` // default folder for TFTP/HTTP/FTP
	ServerIdleMinutes int    `json:"serverIdleMinutes"`

	// Google Sheets import: the last sheet used and the values filled in for
	// rows that lack them, so a re-import (to pick up changed ports) is one click.
	SheetURL         string `json:"sheetUrl"`
	SheetDefaultHost string `json:"sheetDefaultHost"`
	SheetDefaultUser string `json:"sheetDefaultUser"`
	SheetFolder      string `json:"sheetFolder"`
	SheetUseAI       bool   `json:"sheetUseAI"` // parse with AI when an API key is set

	// SessionProbe checks in the background whether sessions are reachable
	// (TCP connect at startup, then every 5 minutes).
	SessionProbe bool `json:"sessionProbe"`

	// AI parsing service: "anthropic" (default) or "openrouter". Each has its
	// own model (empty = default); API keys are kept in the OS credential store,
	// never in settings.
	AIProvider      string `json:"aiProvider"`
	AIModel         string `json:"aiModel"`         // Anthropic model ID
	OpenRouterModel string `json:"openRouterModel"` // OpenRouter model ID, e.g. anthropic/claude-opus-5.5
}

// Font chains that older versions stored as the default. Settings still holding
// them are reset to "" so they follow the per-platform default instead.
const (
	legacyTerminalFont = `Menlo, Consolas, "DejaVu Sans Mono", "Noto Sans Mono CJK TC", monospace`
	legacyEditorFont   = `Menlo, Consolas, "DejaVu Sans Mono", monospace`
)

// DefaultSettings returns sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		Theme:              "system",
		Language:           "system",
		TerminalFontSize:   14,
		Scrollback:         10000,
		CursorBlink:        true,
		SmartCopyPaste:     true,
		TermRightClick:     "menu",
		EditorFontSize:     14,
		EditorTabSize:      4,
		EditorMinimap:      true,
		BigFileLimitMB:     10,
		LogStripANSI:       true,
		ServerIdleMinutes:  30,
		SFTPFollowTerminal: true,
		SFTPAutoSwitch:     true,
		SessionProbe:       true,
	}
}

// Normalize fills defaults for zero values on a session.
func (s *Session) Normalize() {
	switch s.Type {
	case TypeSSH:
		if s.SSH == nil {
			s.SSH = &SSHConfig{}
		}
		if s.SSH.Port == 0 {
			s.SSH.Port = 22
		}
		if s.SSH.Auth == "" {
			s.SSH.Auth = AuthAuto
		}
		for i := range s.SSH.JumpHosts {
			if s.SSH.JumpHosts[i].SessionID == "" && s.SSH.JumpHosts[i].Port == 0 {
				s.SSH.JumpHosts[i].Port = 22
			}
		}
	case TypeSerial:
		if s.Serial == nil {
			s.Serial = &SerialConfig{}
		}
		if s.Serial.Baud == 0 {
			s.Serial.Baud = 115200
		}
		if s.Serial.DataBits == 0 {
			s.Serial.DataBits = 8
		}
		if s.Serial.Parity == "" {
			s.Serial.Parity = "none"
		}
		if s.Serial.StopBits == "" {
			s.Serial.StopBits = "1"
		}
		if s.Serial.SendLineEnd == "" {
			s.Serial.SendLineEnd = "cr"
		}
	case TypeTelnet:
		if s.Telnet == nil {
			s.Telnet = &TelnetConfig{}
		}
		if s.Telnet.Port == 0 {
			s.Telnet.Port = 23
		}
	case TypeLocal:
		if s.Local == nil {
			s.Local = &LocalConfig{}
		}
	case TypeTCP, TypeUDP:
		if s.Net == nil {
			s.Net = &NetConfig{}
		}
		if s.Net.Mode == "" {
			s.Net.Mode = "client"
		}
		if s.Net.Format == "" {
			s.Net.Format = "text"
		}
		if s.Net.AppendLine == "" {
			s.Net.AppendLine = "none"
		}
	}
}
