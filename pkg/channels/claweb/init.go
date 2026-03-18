package claweb

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("claweb", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewClawebChannel(cfg.Channels.Claweb, b)
	})
}
