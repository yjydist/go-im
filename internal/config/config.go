package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 统一配置结构体
type Config struct {
	App       AppConfig       `mapstructure:"app"`
	APIServer APIServerConfig `mapstructure:"api_server"`
	WSServer  WSServerConfig  `mapstructure:"ws_server"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Kafka     KafkaConfig     `mapstructure:"kafka"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Log       LogConfig       `mapstructure:"log"`
}

type AppConfig struct {
	Env      string `mapstructure:"env"`
	ServerID int64  `mapstructure:"server_id"`
}

type APIServerConfig struct {
	Port           int      `mapstructure:"port"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type WSServerConfig struct {
	Port             int      `mapstructure:"port"`
	RPCPort          int      `mapstructure:"rpc_port"`
	RPCAdvertiseAddr string   `mapstructure:"rpc_advertise_addr"` // Docker 等环境中对外通告的 RPC 地址，为空时回退到 localhost:rpc_port
	InternalAPIKey   string   `mapstructure:"internal_api_key"`
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
}

type MySQLConfig struct {
	DSN            string `mapstructure:"dsn"`
	MaxOpenConns   int    `mapstructure:"max_open_conns"`
	MaxIdleConns   int    `mapstructure:"max_idle_conns"`
	ConnMaxLifeSec int    `mapstructure:"conn_max_life_sec"` // 连接最大生命周期（秒），默认 300（5 分钟）
	ConnMaxIdleSec int    `mapstructure:"conn_max_idle_sec"` // 空闲连接最大存活时间（秒），默认 180（3 分钟）
}

type RedisConfig struct {
	Addr           string `mapstructure:"addr"`
	Password       string `mapstructure:"password"`
	DB             int    `mapstructure:"db"`
	PoolSize       int    `mapstructure:"pool_size"`        // 连接池大小，0 表示使用 go-redis 默认值（10 * NumCPU）
	MinIdleConns   int    `mapstructure:"min_idle_conns"`   // 最小空闲连接数
	DialTimeoutMs  int    `mapstructure:"dial_timeout_ms"`  // 拨号超时（毫秒），默认 5000
	ReadTimeoutMs  int    `mapstructure:"read_timeout_ms"`  // 读超时（毫秒），默认 3000
	WriteTimeoutMs int    `mapstructure:"write_timeout_ms"` // 写超时（毫秒），默认 3000
}

type KafkaConfig struct {
	Brokers       []string `mapstructure:"brokers"`
	TopicChat     string   `mapstructure:"topic_chat"`
	ConsumerGroup string   `mapstructure:"consumer_group"`
}

type JWTConfig struct {
	Secret             string `mapstructure:"secret"`
	AccessExpireHours  int    `mapstructure:"access_expire_hours"`
	RefreshExpireHours int    `mapstructure:"refresh_expire_hours"`
}

type LogConfig struct {
	Level    string `mapstructure:"level"`
	Filename string `mapstructure:"filename"`
}

// GlobalConfig 全局配置实例
var GlobalConfig *Config

// Load 加载配置文件，支持环境变量覆盖。
// 环境变量前缀为 GOIM，使用下划线分隔嵌套字段，
// 例如 GOIM_JWT_SECRET 可覆盖 jwt.secret 配置项。
func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 启用环境变量覆盖：GOIM_JWT_SECRET -> jwt.secret
	viper.SetEnvPrefix("GOIM")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config failed: %w", err)
	}

	GlobalConfig = cfg
	return cfg, nil
}

// Validate 校验配置安全性。
// 在 release 环境下强制要求安全配置，在 debug 环境下仅发出警告日志（返回 nil）。
// 调用方应在 Load 之后、启动服务之前调用此方法。
func (c *Config) Validate() error {
	isRelease := c.App.Env == "release"

	// JWT Secret 校验：禁止使用空值或默认占位符
	if c.JWT.Secret == "" || c.JWT.Secret == "changeme" {
		if isRelease {
			return fmt.Errorf("config: jwt.secret must be set to a strong value in release mode (got %q)", c.JWT.Secret)
		}
		// debug 模式：不阻止启动，由调用方决定是否打印警告
	}

	// Internal API Key 校验：release 模式下禁止为空（空值会导致内部推送接口无认证）
	if c.WSServer.InternalAPIKey == "" {
		if isRelease {
			return fmt.Errorf("config: ws_server.internal_api_key must be set in release mode to protect internal push endpoint")
		}
	}

	return nil
}
