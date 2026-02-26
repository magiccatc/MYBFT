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

func NewClient() *Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	return &Client{addr: addr}
}

func (c *Client) cmd(args ...string) (string, error) {
	base := []string{"-h", strings.Split(c.addr, ":")[0], "-p", strings.Split(c.addr, ":")[1]}
	all := append(base, args...)
	out, err := exec.Command("redis-cli", all...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("redis-cli %v: %w: %s", args, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Client) HSet(key string, fields map[string]string) error {
	args := []string{"HSET", key}
	for k, v := range fields {
		args = append(args, k, v)
	}
	_, err := c.cmd(args...)
	return err
}

func (c *Client) HGet(key, field string) (string, error) { return c.cmd("HGET", key, field) }

func (c *Client) HDel(key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	args := []string{"HDEL", key}
	args = append(args, fields...)
	_, err := c.cmd(args...)
	return err
}
func (c *Client) HExists(key, field string) (bool, error) {
	v, err := c.cmd("HEXISTS", key, field)
	return v == "1", err
}
func (c *Client) HIncrBy(key, field string, by int) (int, error) {
	v, err := c.cmd("HINCRBY", key, field, strconv.Itoa(by))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}
func (c *Client) Del(key string) error { _, err := c.cmd("DEL", key); return err }
func (c *Client) SAdd(key, member string) (int, error) {
	v, err := c.cmd("SADD", key, member)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

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
