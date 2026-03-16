package redisx

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Client struct {
	addr string
}

// 创建 Redis 客户端，优先读取 REDIS_ADDR，默认 127.0.0.1:6379。
func NewClient() *Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	return &Client{addr: addr}
}

// 通过 redis-cli 执行命令，作为项目的轻量数据存储入口。
func (c *Client) cmd(args ...string) (string, error) {
	base := []string{"-h", strings.Split(c.addr, ":")[0], "-p", strings.Split(c.addr, ":")[1]}
	all := append(base, args...)
	out, err := exec.Command("redis-cli", all...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("redis-cli %v: %w: %s", args, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// 批量写入 Hash（用于集群配置、密钥与统计指标）。
func (c *Client) HSet(key string, fields map[string]string) error {
	args := []string{"HSET", key}
	for k, v := range fields {
		args = append(args, k, v)
	}
	_, err := c.cmd(args...)
	return err
}

// HGet 读取 Hash 字段值。
func (c *Client) HGet(key, field string) (string, error) { return c.cmd("HGET", key, field) }

// 删除 Hash 字段（如清理延迟统计状态）。
func (c *Client) HDel(key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	args := []string{"HDEL", key}
	args = append(args, fields...)
	_, err := c.cmd(args...)
	return err
}
// 判断 Hash 字段是否存在。
func (c *Client) HExists(key, field string) (bool, error) {
	v, err := c.cmd("HEXISTS", key, field)
	return v == "1", err
}
// 自增 Hash 字段（用于统计 /end 回复数）。
func (c *Client) HIncrBy(key, field string, by int) (int, error) {
	v, err := c.cmd("HINCRBY", key, field, strconv.Itoa(by))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}
// Del 删除指定键。
func (c *Client) Del(key string) error { _, err := c.cmd("DEL", key); return err }
// 向集合写入成员（用于去重）。
func (c *Client) SAdd(key, member string) (int, error) {
	v, err := c.cmd("SADD", key, member)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

// 读取 Hash 全量字段（用于读取集群配置）。
func (c *Client) HGetAll(key string) (map[string]string, error) {
	out, err := c.cmd("HGETALL", key)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return map[string]string{}, nil
	}
	lines := strings.Split(out, "\n")
	res := map[string]string{}
	for i := 0; i+1 < len(lines); i += 2 {
		res[strings.TrimSpace(lines[i])] = strings.TrimSpace(lines[i+1])
	}
	return res, nil
}

type ClusterConfig struct {
	N          int
	BasePort   int
	ClientAddr string
}

// 读取 cluster:config，供节点初始化端口、N 与客户端地址。
func ReadClusterConfig(rdb *Client) (ClusterConfig, error) {
	cfg := ClusterConfig{BasePort: 9000, ClientAddr: "127.0.0.1:8000"}
	m, err := rdb.HGetAll("cluster:config")
	if err != nil {
		return cfg, err
	}
	nRaw, ok := m["N"]
	if !ok || nRaw == "" {
		return cfg, fmt.Errorf("missing cluster:config N")
	}
	n, err := strconv.Atoi(nRaw)
	if err != nil {
		return cfg, err
	}
	cfg.N = n
	if bp := m["basePort"]; bp != "" {
		if v, e := strconv.Atoi(bp); e == nil {
			cfg.BasePort = v
		}
	}
	if ca := m["clientAddr"]; ca != "" {
		cfg.ClientAddr = ca
	}
	return cfg, nil
}
