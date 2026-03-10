package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryCommand(t *testing.T) {
	cmd := NewMemoryCommand()

	require.NotNil(t, cmd)

	assert.Equal(t, "memory", cmd.Use)
	assert.Equal(t, "Memory management commands", cmd.Short)
	assert.False(t, cmd.HasAlias("m"))
	assert.Nil(t, cmd.Run)
	assert.NotNil(t, cmd.RunE)
	assert.True(t, cmd.HasSubCommands())
	assert.Len(t, cmd.Commands(), 3)
}
