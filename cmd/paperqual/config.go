package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const defaultAddress = "127.0.0.1:19081"

type config struct {
	address   string
	dataDir   string
	selfCheck bool
}

func parseConfig(args []string) (config, error) {
	set := flag.NewFlagSet("paperqual", flag.ContinueOnError)
	var cfg config
	set.StringVar(&cfg.address, "addr", "", "HTTP 回环监听地址")
	set.StringVar(&cfg.dataDir, "data", "./paperqual-data", "持久化数据目录")
	set.BoolVar(&cfg.selfCheck, "self-check", false, "执行真实 HTTP 合格流程并退出")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("不支持位置参数")
	}
	if cfg.address == "" {
		port := strings.TrimSpace(os.Getenv("PORT"))
		if port == "" {
			cfg.address = defaultAddress
		} else {
			value, err := strconv.Atoi(port)
			if err != nil || value < 1 || value > 65535 {
				return config{}, fmt.Errorf("PORT 必须是 1 到 65535 的端口号")
			}
			cfg.address = net.JoinHostPort("127.0.0.1", port)
		}
	}
	if err := validateAddress(cfg.address); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("-addr 必须是 host:port：%w", err)
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("-addr 端口必须在 1 到 65535 之间")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-addr 必须使用 127.0.0.1 或 ::1 回环地址")
	}
	return nil
}
