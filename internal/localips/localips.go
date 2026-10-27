// Package localips 枚举本机对外 IPv4 地址（供共享服务器广播与 Host 校验使用）。
//
// 列表**包含所有网卡的全局 IPv4**：VMware / Hyper-V / WSL 的虚拟网卡与
// Windows 移动热点都在内。这不是疏漏——它们同时是 Host 校验白名单，
// 虚拟机、热点上的手机确实会用这些地址访问宿主机，剔掉就会 403。
// 展示层靠"默认路由地址排首位"给出推荐项，见 prioritizeDefaultRoute。
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
// **默认路由的源地址排在首位**（见 prioritizeDefaultRoute）：调用方按第一个
// 地址生成二维码/访问链接，而接口枚举顺序不保证真实网卡在前。
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
	return prioritizeDefaultRoute(filterAddrs(addrs))
}

// defaultRouteSource 返回系统路由表为"访问外网"选中的源地址；拿不到时返回空串。
//
// 用 UDP "连接"取本地地址：UDP 的 connect 只在内核里记一条路由决策，
// **不发送任何数据包**，因此不依赖外网可达、也没有副作用。8.8.8.8 只是
// "一个必然走默认路由的地址"，连不通也不影响结果。
//
// 这台机器上的典型问题是 `net.InterfaceAddrs()` 的顺序按接口索引来，
// Windows 移动热点（192.168.137.1）与 VMware 虚拟网卡常常排在真实 WLAN
// 前面，前端默认取 ips[0] 生成二维码，就会指向一个没开的网段。
func defaultRouteSource() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return addr.IP.String()
}

// prioritizeDefaultRoute 把默认路由的源地址移到首位。
//
// 只改**顺序**、不删地址：顺序决定前端默认选中哪一项（二维码与"打开链接"），
// 整份列表仍然是 Host 白名单，少一项就会让对应的访问方式 403。
// 无网络（没有默认路由）时保持原顺序，不影响启动。
func prioritizeDefaultRoute(ips []string) []string {
	return moveToFront(ips, defaultRouteSource())
}

// moveToFront 把 preferred 移到首位；为空或不在列表里时原样返回。
// 不修改入参——它可能来自共享缓存，就地改动会波及别处。
func moveToFront(ips []string, preferred string) []string {
	if preferred == "" {
		return ips
	}
	for i, ip := range ips {
		if ip != preferred {
			continue
		}
		if i == 0 {
			return ips
		}
		out := make([]string, 0, len(ips))
		out = append(out, ip)
		out = append(out, ips[:i]...)
		out = append(out, ips[i+1:]...)
		return out
	}
	return ips
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
