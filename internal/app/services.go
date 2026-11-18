package app

import "github.com/wailsapp/wails/v3/pkg/application"

// Services returns every Wails-bound service.
func Services(c *Core) []application.Service {
	return []application.Service{
		application.NewService(NewSystemService(c)),
		application.NewService(NewSessionService(c)),
		application.NewService(NewTerminalService(c)),
		application.NewService(NewSerialService(c)),
		application.NewService(NewSFTPService(c)),
		application.NewService(NewEditorService(c)),
		application.NewService(NewTunnelService(c)),
		application.NewService(NewServerService(c)),
		application.NewService(NewImportService(c)),
		application.NewService(NewProbeService(c)),
	}
}
