// Package localips 枚举本机对外 IPv4 地址（供共享服务器广播与 Host 校验使用）。
package localips

import (
	"my-video-go/internal/constants"
	"net"
	"sync"
	"time"
)

// cacheTTL 直接复用 constants.IPCacheTTLSecs，不另立常量。
// （原注释称"避免循环依赖"并不成立：constants 只 import "time"。）
const cacheTTL = constants.IPCacheTTLSecs * time.Second

var (
	mu      sync.Mutex
	cached  []string
	expires time.Time
)

// Get 返回本机全局 IPv4 列表（过滤回环、IPv6、169.254.* 链路本地地址），
// 5 分钟内复用缓存；探测结果为空时回退 ["127.0.0.1"]。
//
// 返回**副本**：调用方会把这些地址长期存进 state.shareInfo.IPs 并作为 Host
// 校验白名单（share.Server.Start 冻结 s.ips），若共享同一个底层数组，
// 任何一处对返回值的修改都会悄悄改写那份"冻结"的白名单。
func Get() []string {
	mu.Lock()
	defer mu.Unlock()
	if cached == nil || !time.Now().Before(expires) {
		cached = detect()
		expires = time.Now().Add(cacheTTL)
	}
	out := make([]string, len(cached))
	copy(out, cached)
	return out
}

func detect() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	return filterAddrs(addrs)
}

// filterAddrs 从网卡地址里挑出可对外广播的 IPv4，过滤回环、链路本地与多播；
// 无可用地址时回退 ["127.0.0.1"]（绝不返回空切片：调用方会直接广播这些地址）。
// 与 detect 分开是为了能脱离真实网卡测试过滤规则。
func filterAddrs(addrs []net.Addr) []string {
	var out []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if len(out) == 0 {
		return []string{"127.0.0.1"}
	}
	return out
}
