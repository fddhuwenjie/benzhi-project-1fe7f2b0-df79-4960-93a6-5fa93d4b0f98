package main

import "testing"

func TestAddressResolution(t *testing.T) {
	t.Setenv("PORT", "")
	cfg, err := parseConfig(nil)
	if err != nil || cfg.address != "127.0.0.1:19081" {
		t.Fatalf("默认地址错误: %+v %v", cfg, err)
	}
	t.Setenv("PORT", "19123")
	cfg, err = parseConfig(nil)
	if err != nil || cfg.address != "127.0.0.1:19123" {
		t.Fatalf("PORT 地址错误: %+v %v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-addr=127.0.0.1:19234"})
	if err != nil || cfg.address != "127.0.0.1:19234" {
		t.Fatalf("显式地址未优先: %+v %v", cfg, err)
	}
}

func TestAddressValidationRejectsExternalAndInvalidPorts(t *testing.T) {
	for _, address := range []string{"0.0.0.0:19081", "192.0.2.1:19081", "127.0.0.1:0", "127.0.0.1:70000", "localhost:19081"} {
		if err := validateAddress(address); err == nil {
			t.Errorf("地址 %s 应被拒绝", address)
		}
	}
	t.Setenv("PORT", "not-a-port")
	if _, err := parseConfig(nil); err == nil {
		t.Fatal("非法 PORT 应被拒绝")
	}
}
