package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	MemoryProviderFile     = "file"
	MemoryProviderMuninnDB = "muninndb"
	DefaultMemoryTimeout   = "30s"
	DefaultMemoryVault     = "default"
)

// MemoryConfig 定义记忆系统配置。
type MemoryConfig struct {
	Provider string            `json:"provider"`
	File     *FileMemoryConfig `json:"file,omitempty"`
	MuninnDB *MuninnDBConfig   `json:"muninndb,omitempty"`
}

type FileMemoryConfig struct {
	Workspace string `json:"workspace"`
}

type MuninnDBConfig struct {
	Endpoint       string `json:"endpoint"`
	Vault          string `json:"vault"`
	APIKey         string `json:"api_key"`
	Timeout        string `json:"timeout"`
	FallbackToFile bool   `json:"fallback_to_file"`
}

func (c *MemoryConfig) ApplyDefaults() {
	if c.Provider == "" {
		c.Provider = MemoryProviderFile
	}
	if c.File == nil {
		c.File = &FileMemoryConfig{}
	}
	if c.MuninnDB == nil {
		c.MuninnDB = &MuninnDBConfig{}
	}
	if c.MuninnDB.Vault == "" {
		c.MuninnDB.Vault = DefaultMemoryVault
	}
	if c.MuninnDB.Timeout == "" {
		c.MuninnDB.Timeout = DefaultMemoryTimeout
	}
}

func (c *MemoryConfig) ExpandEnvVars() {
	if c == nil {
		return
	}
	if c.File != nil {
		c.File.Workspace = expandEnvVars(c.File.Workspace)
	}
	if c.MuninnDB != nil {
		c.MuninnDB.Endpoint = expandEnvVars(c.MuninnDB.Endpoint)
		c.MuninnDB.Vault = expandEnvVars(c.MuninnDB.Vault)
		c.MuninnDB.APIKey = expandEnvVars(c.MuninnDB.APIKey)
		c.MuninnDB.Timeout = expandEnvVars(c.MuninnDB.Timeout)
	}
}

func (c *MemoryConfig) Validate() error {
	if c == nil {
		return nil
	}
	switch c.Provider {
	case "", MemoryProviderFile:
		return nil
	case MemoryProviderMuninnDB:
		if c.MuninnDB == nil {
			return fmt.Errorf("memory.muninndb is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		if strings.TrimSpace(c.MuninnDB.Endpoint) == "" {
			return fmt.Errorf("memory.muninndb.endpoint is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		if strings.TrimSpace(c.MuninnDB.Vault) == "" {
			return fmt.Errorf("memory.muninndb.vault is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		return nil
	default:
		return fmt.Errorf("memory.provider must be %q or %q", MemoryProviderFile, MemoryProviderMuninnDB)
	}
}

// expandEnvVars 展开字符串中的环境变量引用。
func expandEnvVars(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}
